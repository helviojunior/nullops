package cmd

import (
	"errors"

	"github.com/helviojunior/fmweb/internal/ascii"
	"github.com/helviojunior/fmweb/pkg/log"
	"github.com/helviojunior/fmweb/pkg/tlsutil"
	"github.com/spf13/cobra"
)

var tlsOpts = struct {
	Hosts    []string
	Days     int
	Force    bool
	CertFile string
	KeyFile  string
}{}

var createTLSCmd = &cobra.Command{
	Use:   "create-tls",
	Short: "Create (or import) the HTTPS certificate used by the web server",
	Long: ascii.LogoHelp(`Create a self-signed ECDSA P-256 certificate for the web server and store
it in <data-dir>/tls/. Use --cert/--key to import an existing PEM pair instead.

HTTPS matters: the encrypted /api channel relies on WebCrypto, which browsers
only expose on https:// pages (or on http://localhost).`),
	Example: `
   - fmweb create-tls
   - fmweb create-tls --host files.corp.local --host 10.0.0.5 --days 365
   - fmweb create-tls --cert server.crt --key server.key --force`,
	RunE: func(cmd *cobra.Command, args []string) error {
		d, err := dataDir()
		if err != nil {
			return err
		}
		if tlsutil.Exists(d.CertFile(), d.KeyFile()) && !tlsOpts.Force {
			return errors.New("a certificate already exists in " + d.TLSDir() + " (use --force to replace it)")
		}
		if (tlsOpts.CertFile == "") != (tlsOpts.KeyFile == "") {
			return errors.New("--cert and --key must be used together")
		}
		if tlsOpts.CertFile != "" {
			if err := tlsutil.Import(tlsOpts.CertFile, tlsOpts.KeyFile, d.CertFile(), d.KeyFile()); err != nil {
				return err
			}
			log.Info("certificate imported", "path", d.TLSDir())
		} else {
			hosts := tlsOpts.Hosts
			if len(hosts) == 0 {
				hosts = tlsutil.LocalHosts()
			}
			if err := tlsutil.Generate(d.CertFile(), d.KeyFile(), tlsutil.Options{Hosts: hosts, Days: tlsOpts.Days}); err != nil {
				return err
			}
			log.Info("self-signed certificate created", "path", d.TLSDir())
		}
		if desc, err := tlsutil.Describe(d.CertFile()); err == nil {
			log.Info("certificate", "info", desc)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(createTLSCmd)
	createTLSCmd.Flags().StringSliceVar(&tlsOpts.Hosts, "host", nil, "DNS name or IP for the certificate (repeatable; default: local hostname and IPs)")
	createTLSCmd.Flags().IntVar(&tlsOpts.Days, "days", 825, "Validity in days")
	createTLSCmd.Flags().BoolVarP(&tlsOpts.Force, "force", "F", false, "Replace an existing certificate")
	createTLSCmd.Flags().StringVar(&tlsOpts.CertFile, "cert", "", "Import this PEM certificate instead of generating one")
	createTLSCmd.Flags().StringVar(&tlsOpts.KeyFile, "key", "", "PEM private key matching --cert")
}
