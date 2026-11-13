package cmd

import (
	"fmt"
	"os"

	"github.com/helviojunior/fmweb/internal/ascii"
	"github.com/helviojunior/fmweb/pkg/datadir"
	"github.com/helviojunior/fmweb/pkg/log"
	"github.com/spf13/cobra"
)

var globalOpts = struct {
	DataDir string
	Debug   bool
	Silence bool
}{}

var rootCmd = &cobra.Command{
	Use:   "fmweb",
	Short: "fmweb is a stand-alone, encrypted Web File Manager",
	Long:  ascii.Logo(),
	Example: `
   - fmweb create-tls
   - fmweb create-tls --host files.corp.local --host 10.0.0.5
   - fmweb create-tls --cert /etc/ssl/my.crt --key /etc/ssl/my.key

   - fmweb add-user -u admin
   - fmweb add-user -u bob -p 'S3cret!' --readonly
   - fmweb add-user -u bob --update

   - fmweb web
   - fmweb web -r /data -b 0.0.0.0 -p 8443
   - fmweb web -r /srv/share --readonly --show-hidden`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if cmd.CalledAs() != "version" && !globalOpts.Silence {
			fmt.Println(ascii.Logo())
		}
		if globalOpts.Silence {
			log.EnableSilence()
		}
		if globalOpts.Debug && !globalOpts.Silence {
			log.EnableDebug()
			log.Debug("debug logging enabled")
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
	rootCmd.PersistentFlags().BoolVarP(&globalOpts.Debug, "debug-log", "D", false, "Enable debug logging")
	rootCmd.PersistentFlags().BoolVarP(&globalOpts.Silence, "quiet", "q", false, "Silence (almost all) logging")
}
