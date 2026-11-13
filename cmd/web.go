package cmd

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/helviojunior/fmweb/internal/ascii"
	"github.com/helviojunior/fmweb/pkg/assets"
	"github.com/helviojunior/fmweb/pkg/auth"
	"github.com/helviojunior/fmweb/pkg/fileops"
	"github.com/helviojunior/fmweb/pkg/log"
	"github.com/helviojunior/fmweb/pkg/server"
	"github.com/helviojunior/fmweb/pkg/tlsutil"
	"github.com/spf13/cobra"
)

var webOpts = struct {
	Root       string
	Bind       string
	Port       int
	NoTLS      bool
	NoAuth     bool
	ReadOnly   bool
	ShowHidden bool
	Exclude    []string
	AllowExt   []string
	Title      string
	SessionTTL int
}{}

var webCmd = &cobra.Command{
	Use:   "web",
	Short: "Start the web file manager",
	Long: ascii.LogoHelp(`Start the HTTPS web server. The UI is embedded in the binary and served
from memory. Every /api payload is encrypted end-to-end (ECDH P-256 +
AES-256-GCM, per-session keys kept only in RAM).

On first run, if <data-dir> has no certificate one is generated, and if it has
no user an "admin" account with a random password is created and printed once.`),
	Example: `
   - fmweb web
   - fmweb web -r /data -b 0.0.0.0 -p 8443
   - fmweb web -r /srv/share --readonly --exclude '*.key' --exclude node_modules`,
	RunE: runWeb,
}

func runWeb(cmd *cobra.Command, args []string) error {
	if err := assets.Load(); err != nil {
		return fmt.Errorf("embedded front-end: %w", err)
	}
	fsys, err := fileops.New(webOpts.Root, fileops.Options{
		ShowHidden: webOpts.ShowHidden, Exclude: webOpts.Exclude,
	})
	if err != nil {
		return fmt.Errorf("root directory: %w", err)
	}
	d, err := dataDir()
	if err != nil {
		return err
	}

	// users
	store, err := auth.Open(d.UsersFile())
	if err != nil {
		return err
	}
	var genUser, genPass string
	if !webOpts.NoAuth && store.Count() == 0 {
		genUser = "admin"
		if genPass, err = auth.GeneratePassword(20); err != nil {
			return err
		}
		if err := store.Set(genUser, genPass, false, false); err != nil {
			return err
		}
		log.Warn("no users found: created the initial account", "user", genUser, "file", d.UsersFile())
	}

	// TLS
	var cert *tls.Certificate
	if !webOpts.NoTLS {
		if !tlsutil.Exists(d.CertFile(), d.KeyFile()) {
			hosts := tlsutil.LocalHosts()
			if ip := net.ParseIP(webOpts.Bind); ip != nil && !ip.IsUnspecified() {
				hosts = append(hosts, webOpts.Bind)
			}
			if err := tlsutil.Generate(d.CertFile(), d.KeyFile(), tlsutil.Options{Hosts: hosts}); err != nil {
				return fmt.Errorf("generating certificate: %w", err)
			}
			log.Warn("no certificate found: generated a self-signed one (see `fmweb create-tls`)", "path", d.TLSDir())
		}
		c, err := tlsutil.Load(d.CertFile(), d.KeyFile())
		if err != nil {
			return fmt.Errorf("loading certificate from %s: %w", d.TLSDir(), err)
		}
		cert = &c
	}

	addr := net.JoinHostPort(webOpts.Bind, strconv.Itoa(webOpts.Port))
	srv, err := server.New(server.Config{
		Addr:       addr,
		TLS:        cert,
		NoAuth:     webOpts.NoAuth,
		ReadOnly:   webOpts.ReadOnly,
		Title:      webOpts.Title,
		SessionTTL: time.Duration(webOpts.SessionTTL) * time.Second,
		AllowedExt: webOpts.AllowExt,
		Users:      store,
		FS:         fsys,
	})
	if err != nil {
		return err
	}

	printBanner(fsys.Root(), cert != nil, genUser, genPass, store.Names())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err = srv.ListenAndServe(ctx)
	if ctx.Err() != nil {
		fmt.Println()
		log.Info("stopped")
	}
	return err
}

func printBanner(root string, https bool, genUser, genPass string, users []string) {
	scheme := "http"
	if https {
		scheme = "https"
	}
	port := strconv.Itoa(webOpts.Port)
	var urls []string
	ip := net.ParseIP(webOpts.Bind)
	if ip != nil && ip.IsUnspecified() {
		for _, h := range tlsutil.LocalHosts() {
			if a := net.ParseIP(h); a != nil && a.To4() != nil {
				urls = append(urls, scheme+"://"+net.JoinHostPort(h, port)+"/")
			}
		}
	} else {
		urls = append(urls, scheme+"://"+net.JoinHostPort(webOpts.Bind, port)+"/")
	}

	fmt.Println()
	fmt.Printf("  \033[36mserving\033[0m   %s\n", root)
	if webOpts.ReadOnly {
		fmt.Printf("  \033[33mread-only mode\033[0m\n")
	}
	for _, u := range urls {
		fmt.Printf("  \033[32mURL\033[0m       \033[1m%s\033[0m\n", u)
	}
	fmt.Println()
	switch {
	case webOpts.NoAuth:
		fmt.Printf("  \033[33mauthentication disabled (--no-auth)\033[0m\n")
	case genUser != "":
		fmt.Printf("  \033[32muser\033[0m      %s\n", genUser)
		fmt.Printf("  \033[32mpass\033[0m      %s   \033[90m(generated now, shown only once)\033[0m\n", genPass)
	default:
		fmt.Printf("  \033[32musers\033[0m     %d configured (fmweb add-user to manage)\n", len(users))
	}
	fmt.Println()
	fmt.Printf("  \033[90mtransport: ECDH P-256 + AES-256-GCM per session (keys in memory only)\033[0m\n")
	fmt.Printf("  \033[90mfront-end: %d embedded files served from memory\033[0m\n", assets.Count())
	if https {
		fmt.Printf("  \033[90mhttps: a self-signed certificate triggers a one-time browser warning\033[0m\n")
	} else {
		fmt.Printf("  \033[33mWARNING: TLS disabled. The UI only works on https:// or http://localhost\033[0m\n")
	}
	fmt.Printf("  \033[90mpress Ctrl+C to stop\033[0m\n\n")
}

func init() {
	rootCmd.AddCommand(webCmd)
	f := webCmd.Flags()
	f.StringVarP(&webOpts.Root, "root", "r", ".", "Directory to serve")
	f.StringVarP(&webOpts.Bind, "bind", "b", "127.0.0.1", "Bind address (0.0.0.0 for all interfaces)")
	f.IntVarP(&webOpts.Port, "port", "p", 8181, "TCP port")
	f.BoolVar(&webOpts.NoTLS, "no-tls", false, "Serve plain http:// (the UI then only works on localhost)")
	f.BoolVar(&webOpts.NoAuth, "no-auth", false, "Disable authentication (dangerous)")
	f.BoolVar(&webOpts.ReadOnly, "readonly", false, "Serve read-only for every user")
	f.BoolVar(&webOpts.ShowHidden, "show-hidden", false, "Show dotfiles")
	f.StringArrayVar(&webOpts.Exclude, "exclude", nil, "Hide entries matching this glob (repeatable)")
	f.StringSliceVar(&webOpts.AllowExt, "allow-ext", nil, "Only allow uploads with these extensions (e.g. pdf,txt)")
	f.StringVar(&webOpts.Title, "title", "Web File Manager", "Title shown in the UI")
	f.IntVar(&webOpts.SessionTTL, "session-ttl", 3600, "Idle session lifetime in seconds")
}
