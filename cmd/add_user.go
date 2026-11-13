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
	Username string
	Password string
	ReadOnly bool
	Update   bool
	Generate bool
}{}

var addUserCmd = &cobra.Command{
	Use:   "add-user",
	Short: "Add (or update) a user allowed to sign in",
	Long: ascii.LogoHelp(`Add a user to <data-dir>/users.json. Passwords are stored as scrypt hashes.
Without -p the password is read from the terminal (or generated with --generate).`),
	Example: `
   - fmweb add-user -u admin
   - fmweb add-user -u bob -p 'S3cret!' --readonly
   - fmweb add-user -u bob --generate --update`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := auth.ValidateUsername(userOpts.Username); err != nil {
			return err
		}
		d, err := dataDir()
		if err != nil {
			return err
		}
		store, err := auth.Open(d.UsersFile())
		if err != nil {
			return err
		}
		if _, exists := store.Get(userOpts.Username); exists && !userOpts.Update {
			return fmt.Errorf("user %q already exists (use --update to change it)", userOpts.Username)
		}

		password := userOpts.Password
		generated := false
		switch {
		case password != "":
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
		if err := store.Set(userOpts.Username, password, userOpts.ReadOnly, userOpts.Update); err != nil {
			return err
		}
		log.Info("user saved", "user", userOpts.Username, "readonly", userOpts.ReadOnly, "file", d.UsersFile())
		if generated {
			fmt.Printf("\n  user: %s\n  pass: %s\n\n", userOpts.Username, password)
		}
		return nil
	},
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
	addUserCmd.Flags().StringVarP(&userOpts.Username, "user", "u", "", "Username")
	addUserCmd.Flags().StringVarP(&userOpts.Password, "pass", "p", "", "Password (prompted when omitted; visible in shell history!)")
	addUserCmd.Flags().BoolVarP(&userOpts.Generate, "generate", "g", false, "Generate a random password and print it")
	addUserCmd.Flags().BoolVar(&userOpts.ReadOnly, "readonly", false, "Read-only user (cannot modify files)")
	addUserCmd.Flags().BoolVar(&userOpts.Update, "update", false, "Update an existing user")
	_ = addUserCmd.MarkFlagRequired("user")
}
