package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/helviojunior/fmweb/internal/ascii"
	"github.com/helviojunior/fmweb/pkg/auth"
	"github.com/helviojunior/fmweb/pkg/log"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var userOpts = struct {
	Username    string
	Password    string
	ReadOnly    bool
	Update      bool
	Generate    bool
	SetPassword bool
	MFA         bool
	NoMFA       bool
}{}

var addUserCmd = &cobra.Command{
	Use:   "add-user",
	Short: "Add (or update) a user allowed to sign in",
	Long: ascii.LogoHelp(`Add a user to <data-dir>/users.json. Passwords are stored as scrypt hashes.
Without -p the password is read from the terminal (or generated with --generate).

MFA (TOTP) is optional: --mfa enables it and prints a QR code to scan with an
authenticator app; --no-mfa turns it off. Users can also enable it themselves
in the web interface. With --update, the password is kept unless -p,
--generate or --set-password is given. A running server picks up the change
within a few seconds.`),
	Example: `
   - fmweb add-user -u admin
   - fmweb add-user -u bob -p 'S3cret!' --readonly
   - fmweb add-user -u bob --mfa
   - fmweb add-user -u bob --update --mfa
   - fmweb add-user -u bob --update --no-mfa
   - fmweb add-user -u bob --update --set-password`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := auth.ValidateUsername(userOpts.Username); err != nil {
			return err
		}
		if userOpts.MFA && userOpts.NoMFA {
			return errors.New("--mfa and --no-mfa are mutually exclusive")
		}
		d, err := dataDir()
		if err != nil {
			return err
		}
		store, err := auth.Open(d.UsersFile())
		if err != nil {
			return err
		}
		existing, exists := store.Get(userOpts.Username)
		if exists && !userOpts.Update {
			return fmt.Errorf("user %q already exists (use --update to change it)", userOpts.Username)
		}
		if !exists && userOpts.Update {
			return fmt.Errorf("user %q not found", userOpts.Username)
		}

		changePassword := !exists || userOpts.Password != "" || userOpts.Generate || userOpts.SetPassword
		readonly := userOpts.ReadOnly
		if exists && !cmd.Flags().Changed("readonly") {
			readonly = existing.ReadOnly
		}

		password, generated := "", false
		if changePassword {
			switch {
			case userOpts.Password != "":
				password = userOpts.Password
			case userOpts.Generate:
				if password, err = auth.GeneratePassword(20); err != nil {
					return err
				}
				generated = true
			default:
				if password, err = promptPassword(); err != nil {
					return err
				}
			}
			if len(password) < 8 {
				return errors.New("password must have at least 8 characters")
			}
			if err := store.Set(userOpts.Username, password, readonly, exists); err != nil {
				return err
			}
		} else if readonly != existing.ReadOnly {
			if err := store.SetReadOnly(userOpts.Username, readonly); err != nil {
				return err
			}
		}

		var secret string
		switch {
		case userOpts.MFA:
			if secret, err = auth.NewTOTPSecret(); err != nil {
				return err
			}
			if err := store.SetMFA(userOpts.Username, secret); err != nil {
				return err
			}
		case userOpts.NoMFA:
			if err := store.SetMFA(userOpts.Username, ""); err != nil {
				return err
			}
		}

		u, _ := store.Get(userOpts.Username)
		log.Info("user saved", "user", u.Username, "readonly", u.ReadOnly, "mfa", u.HasMFA(), "file", d.UsersFile())
		if generated {
			fmt.Printf("\n  user: %s\n  pass: %s\n\n", userOpts.Username, password)
		}
		if secret != "" {
			printMFAEnrollment(userOpts.Username, secret)
		}
		return nil
	},
}

func printMFAEnrollment(user, secret string) {
	uri := auth.TOTPURI("Web File Manager", user, secret)
	fmt.Println()
	fmt.Println("  Scan this QR code with an authenticator app (Google Authenticator, Authy, 1Password, …):")
	fmt.Println()
	if qr, err := auth.QRTerminal(uri); err == nil {
		fmt.Print(qr)
	}
	fmt.Println()
	fmt.Printf("  secret: %s\n", secret)
	fmt.Printf("  uri:    %s\n\n", uri)
}

func promptPassword() (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", errors.New("no terminal to read the password from: use -p or --generate")
	}
	fmt.Fprint(os.Stderr, "Password: ")
	p1, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	fmt.Fprint(os.Stderr, "Confirm password: ")
	p2, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	if string(p1) != string(p2) {
		return "", errors.New("passwords do not match")
	}
	return string(p1), nil
}

func init() {
	rootCmd.AddCommand(addUserCmd)
	f := addUserCmd.Flags()
	f.StringVarP(&userOpts.Username, "user", "u", "", "Username")
	f.StringVarP(&userOpts.Password, "pass", "p", "", "Password (prompted when omitted; visible in shell history!)")
	f.BoolVarP(&userOpts.Generate, "generate", "g", false, "Generate a random password and print it")
	f.BoolVar(&userOpts.SetPassword, "set-password", false, "With --update: prompt for a new password")
	f.BoolVar(&userOpts.ReadOnly, "readonly", false, "Read-only user (cannot modify files)")
	f.BoolVar(&userOpts.Update, "update", false, "Update an existing user")
	f.BoolVar(&userOpts.MFA, "mfa", false, "Enable MFA (TOTP) and print the enrollment QR code")
	f.BoolVar(&userOpts.NoMFA, "no-mfa", false, "Disable MFA")
	_ = addUserCmd.MarkFlagRequired("user")
}
