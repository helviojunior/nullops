package cmd

import (
	"fmt"

	"github.com/helviojunior/nullops/internal/ascii"
	"github.com/helviojunior/nullops/pkg/auth"
	"github.com/spf13/cobra"
)

var mfaQRUser string

var mfaQRCmd = &cobra.Command{
	Use:   "mfa-qr",
	Short: "Show the MFA (TOTP) QR code of a user in the terminal",
	Long: ascii.LogoHelp(`Print the TOTP enrollment QR code of a user that already has MFA, as ASCII
art, together with the setup key and the otpauth:// URI. Use it to add the
account to a new authenticator app without changing the secret.

The QR code contains the user's secret: show it only to that user. To issue a
new secret (and invalidate the old one) use: nullops add-user -u <name> --update --mfa`),
	Example: `
   - nullops mfa-qr -u bob`,
	RunE: func(cmd *cobra.Command, args []string) error {
		d, err := dataDir()
		if err != nil {
			return err
		}
		store, err := auth.Open(d.UsersFile())
		if err != nil {
			return err
		}
		u, ok := store.Get(mfaQRUser)
		if !ok {
			return fmt.Errorf("user %q not found", mfaQRUser)
		}
		if !u.HasMFA() {
			return fmt.Errorf("user %q has no MFA; enable it with: nullops add-user -u %s --update --mfa", u.Username, u.Username)
		}
		printMFAEnrollment(u.Username, u.MFASecret)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(mfaQRCmd)
	mfaQRCmd.Flags().StringVarP(&mfaQRUser, "user", "u", "", "Username")
	_ = mfaQRCmd.MarkFlagRequired("user")
}
