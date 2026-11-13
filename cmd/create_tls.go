package cmd

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/helviojunior/nullops/internal/ascii"
	"github.com/helviojunior/nullops/pkg/log"
	"github.com/helviojunior/nullops/pkg/tlsutil"
	"github.com/spf13/cobra"
)

var tlsOpts = struct {
	Hosts    []string
	Days     int
	Force    bool
	CertFile string
	KeyFile  string

	ACME          bool
	Domains       []string
	ACMEEmail     string
	ACMEStaging   bool
	ACMEHTTPAddr  string
	ACMEDirectory string
}{}

var createTLSCmd = &cobra.Command{
	Use:   "create-tls",
	Short: "Create (or import) the HTTPS certificate used by the web server",
	Long: ascii.LogoHelp(`Create a self-signed ECDSA P-256 certificate for the web server and store
it in <data-dir>/tls/. Use --cert/--key to import an existing PEM pair instead.

Use --acme to obtain a publicly trusted certificate from Let's Encrypt (or any
ACME CA via --acme-directory) with the HTTP-01 challenge. Every --domain must
resolve to this host and port 80 must be reachable from the Internet; a
temporary HTTP server answers the challenge on --acme-http-addr (default :80,
which usually requires root or CAP_NET_BIND_SERVICE) and is stopped afterwards.
The ACME account key is kept in <data-dir>/tls/acme_account.key and reused.
Let's Encrypt certificates last 90 days: to renew, run the same command again
with --force (e.g. from cron). Try --acme-staging first to avoid rate limits.

HTTPS matters: the encrypted /api channel relies on WebCrypto, which browsers
only expose on https:// pages (or on http://localhost).`),
	Example: `
   - nullops create-tls
   - nullops create-tls --host files.corp.local --host 10.0.0.5 --days 365
   - nullops create-tls --cert server.crt --key server.key --force
   - nullops create-tls --acme --domain files.example.com --acme-email admin@example.com
   - nullops create-tls --acme --domain files.example.com --domain www.files.example.com --acme-staging
   - nullops create-tls --acme --domain files.example.com --acme-http-addr :8080 --force   # renew; port 80 forwarded to 8080`,
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
		if tlsOpts.ACME {
			if tlsOpts.CertFile != "" {
				return errors.New("--acme cannot be combined with --cert/--key")
			}
			if len(tlsOpts.Domains) == 0 {
				return errors.New("--acme requires at least one --domain")
			}
			parent := cmd.Context()
			if parent == nil {
				parent = context.Background()
			}
			ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
			defer cancel()
			err := tlsutil.ObtainACME(ctx, tlsutil.ACMEOptions{
				Domains:        tlsOpts.Domains,
				Email:          tlsOpts.ACMEEmail,
				DirectoryURL:   tlsOpts.ACMEDirectory,
				Staging:        tlsOpts.ACMEStaging,
				HTTPAddr:       tlsOpts.ACMEHTTPAddr,
				CertFile:       d.CertFile(),
				KeyFile:        d.KeyFile(),
				AccountKeyFile: filepath.Join(d.TLSDir(), "acme_account.key"),
			})
			if err != nil {
				return err
			}
			log.Info("ACME certificate obtained", "path", d.TLSDir())
		} else if len(tlsOpts.Domains) > 0 {
			return errors.New("--domain is only used with --acme (use --host for self-signed certificates)")
		} else if tlsOpts.CertFile != "" {
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
	createTLSCmd.Flags().BoolVar(&tlsOpts.ACME, "acme", false, "Obtain a trusted certificate from Let's Encrypt / ACME (HTTP-01)")
	createTLSCmd.Flags().StringSliceVar(&tlsOpts.Domains, "domain", nil, "Public DNS name for the ACME certificate (repeatable; required with --acme)")
	createTLSCmd.Flags().StringVar(&tlsOpts.ACMEEmail, "acme-email", "", "Contact e-mail for the ACME account (expiry notices)")
	createTLSCmd.Flags().BoolVar(&tlsOpts.ACMEStaging, "acme-staging", false, "Use the Let's Encrypt staging CA (untrusted certs, for testing)")
	createTLSCmd.Flags().StringVar(&tlsOpts.ACMEHTTPAddr, "acme-http-addr", tlsutil.DefaultACMEHTTPAddr, "Listen address for the temporary HTTP-01 challenge server")
	createTLSCmd.Flags().StringVar(&tlsOpts.ACMEDirectory, "acme-directory", "", "Custom ACME directory URL (overrides --acme-staging)")
	createTLSCmd.MarkFlagsMutuallyExclusive("acme", "cert")
	createTLSCmd.MarkFlagsMutuallyExclusive("acme", "key")
}
