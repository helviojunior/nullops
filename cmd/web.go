package cmd

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/helviojunior/nullops/internal/ascii"
	"github.com/helviojunior/nullops/pkg/assets"
	"github.com/helviojunior/nullops/pkg/auth"
	"github.com/helviojunior/nullops/pkg/fileops"
	"github.com/helviojunior/nullops/pkg/log"
	"github.com/helviojunior/nullops/pkg/pads"
	"github.com/helviojunior/nullops/pkg/pathref"
	"github.com/helviojunior/nullops/pkg/server"
	"github.com/helviojunior/nullops/pkg/session"
	"github.com/helviojunior/nullops/pkg/tlsutil"
	"github.com/helviojunior/nullops/pkg/uploadreq"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var webOpts = struct {
	Root       string
	Bind       string
	Port       int
	NoTLS      bool
	AssumeYes  bool
	NoAuth     bool
	ReadOnly   bool
	ShowHidden bool
	Exclude    []string
	AllowExt   []string
	Title      string
	SessionTTL int
	AuditLog   string
	Trusted    []string
	MaxUpload  string
}{}

var webCmd = &cobra.Command{
	Use:   "web",
	Short: "Start NullOps - Web Resource Manager",
	Long: ascii.LogoHelp(`Start the HTTPS web server. The UI is embedded in the binary and served
from memory. Every /api payload is encrypted end-to-end (ECDH P-256 +
AES-256-GCM, one key per session).

On first run, if <data-dir> has no certificate one is generated, and if it has
no user an "admin" account with a random password is created and printed once.`),
	Example: `
   - nullops web
   - nullops web -r /data -b 0.0.0.0 -p 8443
   - nullops web -r /srv/share --readonly --exclude '*.key' --exclude node_modules
   - nullops web --no-tls          (asks for confirmation: disables payload encryption)`,
	RunE: runWeb,
}

func runWeb(cmd *cobra.Command, args []string) error {
	if webOpts.NoTLS {
		if err := confirmPlaintext(); err != nil {
			return err
		}
	}
	if err := assets.Load(); err != nil {
		return fmt.Errorf("embedded front-end: %w", err)
	}
	log.EnableTimestamps()
	trusted, err := server.ParseTrustedProxies(webOpts.Trusted)
	if err != nil {
		return fmt.Errorf("--trusted-proxy: %w", err)
	}
	maxUpload, err := parseSize(webOpts.MaxUpload)
	if err != nil {
		return fmt.Errorf("--max-upload: %w", err)
	}
	if webOpts.AuditLog != "" {
		if err := log.OpenAuditFile(webOpts.AuditLog); err != nil {
			return fmt.Errorf("--audit-log: %w", err)
		}
		defer log.CloseAuditFile()
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
			log.Warn("no certificate found: generated a self-signed one (see `nullops create-tls`)", "path", d.TLSDir())
		}
		c, err := tlsutil.Load(d.CertFile(), d.KeyFile())
		if err != nil {
			return fmt.Errorf("loading certificate from %s: %w", d.TLSDir(), err)
		}
		cert = &c
	}

	// one SQLite database: sessions (survive restarts, 12h by default) and
	// PastePad metadata + per-pad AES keys
	db, err := session.OpenSQLite(d.DatabaseFile())
	if err != nil {
		return fmt.Errorf("database %s: %w", d.DatabaseFile(), err)
	}
	defer db.Close()
	var sessStore session.Store
	if !webOpts.NoAuth {
		sessStore = db
	}
	padStore, err := pads.OpenStore(db.DB(), d.PadsDir())
	if err != nil {
		return fmt.Errorf("pads: %w", err)
	}
	padMgr := pads.NewManager(padStore)
	uploadReqs, err := uploadreq.Open(db.DB())
	if err != nil {
		return fmt.Errorf("upload requests: %w", err)
	}
	uploadReqs.Purge(time.Now())
	pathRefs, err := pathref.Open(db.DB())
	if err != nil {
		return fmt.Errorf("path refs: %w", err)
	}
	pathRefs.Purge(time.Now().Add(-pathref.Retention))

	addr := net.JoinHostPort(webOpts.Bind, strconv.Itoa(webOpts.Port))
	srv, err := server.New(server.Config{
		Addr:           addr,
		TLS:            cert,
		Plaintext:      webOpts.NoTLS,
		NoAuth:         webOpts.NoAuth,
		ReadOnly:       webOpts.ReadOnly,
		Title:          webOpts.Title,
		SessionTTL:     time.Duration(webOpts.SessionTTL) * time.Second,
		Sessions:       sessStore,
		TrustedProxies: trusted,
		MaxUpload:      maxUpload,
		Pads:           padMgr,
		UploadReqs:     uploadReqs,
		PathRefs:       pathRefs,
		AllowedExt:     webOpts.AllowExt,
		Users:          store,
		FS:             fsys,
	})
	if err != nil {
		return err
	}

	printBanner(fsys.Root(), cert != nil, genUser, genPass, store.Names())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	padsDone := make(chan struct{})
	go func() {
		padMgr.Run(ctx) // persists pads every few seconds, flushes on exit
		close(padsDone)
	}()
	err = srv.ListenAndServe(ctx)
	stop()
	<-padsDone
	if ctx.Err() != nil {
		fmt.Println()
		log.Info("stopped")
	}
	return err
}

var errAborted = errors.New("aborted by user")

// confirmPlaintext warns that --no-tls also disables the payload encryption
// and asks the operator to confirm ([y/N], default no).
func confirmPlaintext() error {
	w := os.Stderr
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  \033[31m!! WARNING: --no-tls\033[0m")
	fmt.Fprintln(w, "  Without TLS the browser does not expose WebCrypto outside localhost, so the")
	fmt.Fprintln(w, "  \033[1mpayload encryption of the /api channel (ECDH + AES-256-GCM) is DISABLED too.\033[0m")
	fmt.Fprintln(w, "  That encryption is what keeps a proxy, IDS/IPS or endpoint agent (EDR/DLP)")
	fmt.Fprintln(w, "  from identifying the traffic: logins, passwords, file names and file contents")
	fmt.Fprintln(w, "  will travel in CLEAR TEXT and can be read or altered by anyone on the path.")
	fmt.Fprintln(w)
	if webOpts.AssumeYes {
		log.Warn("payload encryption disabled (--no-tls --yes)")
		return nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("refusing to start without encryption: no terminal to confirm (use --yes to accept)")
	}
	fmt.Fprint(w, "  Do you want to continue? [y/N] ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes", "s", "sim":
		fmt.Fprintln(w)
		log.Warn("payload encryption disabled by the operator")
		return nil
	}
	return errAborted
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
		fmt.Printf("  \033[32musers\033[0m     %d configured (nullops add-user to manage)\n", len(users))
	}
	fmt.Println()
	if https {
		fmt.Printf("  \033[90mtransport: ECDH P-256 + AES-256-GCM, one key per session\033[0m\n")
	}
	fmt.Printf("  \033[90mfront-end: %d embedded files served from memory\033[0m\n", assets.Count())
	if !webOpts.NoAuth {
		fmt.Printf("  \033[90msessions: valid for %s after login, kept across restarts\033[0m\n",
			(time.Duration(webOpts.SessionTTL) * time.Second).String())
	}
	if https {
		fmt.Printf("  \033[90mhttps: a self-signed certificate triggers a one-time browser warning\033[0m\n")
	} else {
		fmt.Printf("  \033[31mWARNING: TLS and payload encryption are DISABLED (--no-tls).\033[0m\n")
		fmt.Printf("  \033[31m         Credentials, file names and contents travel in clear text.\033[0m\n")
	}
	fmt.Printf("  \033[90mpress Ctrl+C to stop\033[0m\n\n")
}

func init() {
	rootCmd.AddCommand(webCmd)
	f := webCmd.Flags()
	f.StringVarP(&webOpts.Root, "root", "r", ".", "Directory to serve")
	f.StringVarP(&webOpts.Bind, "bind", "b", "127.0.0.1", "Bind address (0.0.0.0 for all interfaces)")
	f.IntVarP(&webOpts.Port, "port", "p", 8181, "TCP port")
	f.BoolVar(&webOpts.NoTLS, "no-tls", false, "Serve plain http:// and DISABLE the payload encryption (asks for confirmation)")
	f.BoolVarP(&webOpts.AssumeYes, "yes", "y", false, "Do not ask for confirmation (with --no-tls)")
	f.BoolVar(&webOpts.NoAuth, "no-auth", false, "Disable authentication (dangerous)")
	f.BoolVar(&webOpts.ReadOnly, "readonly", false, "Serve read-only for every user")
	f.BoolVar(&webOpts.ShowHidden, "show-hidden", false, "Show dotfiles")
	f.StringArrayVar(&webOpts.Exclude, "exclude", nil, "Hide entries matching this glob (repeatable)")
	f.StringSliceVar(&webOpts.AllowExt, "allow-ext", nil, "Only allow uploads with these extensions (e.g. pdf,txt)")
	f.StringVar(&webOpts.Title, "title", "NullOps - Web Resource Manager", "Title shown in the UI")
	f.StringVar(&webOpts.AuditLog, "audit-log", "", "Also write audit events (access, login, file operations) as JSON lines to this file")
	f.StringSliceVar(&webOpts.Trusted, "trusted-proxy", nil, "IP/CIDR of a reverse proxy whose X-Forwarded-For / X-Real-IP is trusted (repeatable)")
	f.StringVar(&webOpts.MaxUpload, "max-upload", "0", "Maximum size of one uploaded file, e.g. 500M, 2G (0 = unlimited)")
	f.IntVar(&webOpts.SessionTTL, "session-ttl", 12*3600, "Session lifetime in seconds after login (default 12h); sessions are kept in <data-dir>/nullops.db")
}

// parseSize parses "0", "1024", "500K", "500M", "2G", "1T" (binary units).
func parseSize(v string) (int64, error) {
	v = strings.TrimSpace(strings.ToUpper(v))
	v = strings.TrimSuffix(strings.TrimSuffix(v, "B"), "I")
	if v == "" {
		return 0, nil
	}
	mult := int64(1)
	switch v[len(v)-1] {
	case 'K':
		mult = 1 << 10
	case 'M':
		mult = 1 << 20
	case 'G':
		mult = 1 << 30
	case 'T':
		mult = 1 << 40
	}
	if mult > 1 {
		v = v[:len(v)-1]
	}
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid size %q", v)
	}
	return n * mult, nil
}
