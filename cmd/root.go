package cmd

import (
	"fmt"
	"os"

	"github.com/helviojunior/nullops/internal/ascii"
	"github.com/helviojunior/nullops/pkg/datadir"
	"github.com/helviojunior/nullops/pkg/log"
	"github.com/spf13/cobra"
)

var globalOpts = struct {
	DataDir string
	Debug   bool
	Verbose int
	Silence bool
}{}

var rootCmd = &cobra.Command{
	Use:   "nullops",
	Short: "nullops (NullOps - Web Resource Manager) is a stand-alone, encrypted web resource manager",
	Long:  ascii.Logo(),
	Example: `
   - nullops create-tls
   - nullops create-tls --host files.corp.local --host 10.0.0.5
   - nullops create-tls --cert /etc/ssl/my.crt --key /etc/ssl/my.key

   - nullops add-user -u admin
   - nullops add-user -u bob -p 'S3cret!' --readonly
   - nullops add-user -u bob --update

   - nullops web
   - nullops web -r /data -b 0.0.0.0 -p 8443
   - nullops web -r /srv/share --readonly --show-hidden`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if cmd.CalledAs() != "version" && !globalOpts.Silence {
			fmt.Println(ascii.Logo())
		}
		v := globalOpts.Verbose
		if globalOpts.Debug && v < 2 {
			v = 2
		}
		switch {
		case globalOpts.Silence:
			log.EnableSilence()
		default:
			log.SetVerbosity(v)
			if v > 0 {
				log.Debug("verbose logging enabled", "level", v)
			}
		}
		return nil
	},
}

// dataDir resolves and creates the data directory.
func dataDir() (*datadir.Dir, error) {
	d, err := datadir.Resolve(globalOpts.DataDir)
	if err != nil {
		return nil, err
	}
	if err := d.Ensure(); err != nil {
		return nil, err
	}
	return d, nil
}

// Execute runs the CLI.
func Execute() {
	if err := ascii.SetConsoleColors(); err != nil {
		log.Debug("failed to set console colors", "err", err)
	}
	rootCmd.CompletionOptions.DisableDefaultCmd = true
	rootCmd.SilenceErrors = true
	rootCmd.SilenceUsage = true
	if err := rootCmd.Execute(); err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&globalOpts.DataDir, "data-dir", datadir.Default,
		"Directory holding the TLS certificate/key and the user database")
	rootCmd.PersistentFlags().CountVarP(&globalOpts.Verbose, "verbose", "v", "Verbosity level: -v debug (TLS/HTTP errors, read calls), -vv trace (every request)")
	rootCmd.PersistentFlags().BoolVarP(&globalOpts.Debug, "debug-log", "D", false, "Enable full debug logging (same as -vv)")
	rootCmd.PersistentFlags().BoolVarP(&globalOpts.Silence, "quiet", "q", false, "Silence (almost all) logging")
}
