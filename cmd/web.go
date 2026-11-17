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
	"github.com/spf13/pflag"
	"golang.org/x/term"
)

var webOpts = struct {
	Root           string
	Bind           string
	Port           int
	NoTLS          bool
	AllowCleartext bool
	AssumeYes      bool
	NoAuth         bool
	ReadOnly       bool
	ShowHidden     bool
	Exclude        []string
	AllowExt       []string
	Title          string
	SessionTTL     int
	AuditLog       string
	Trusted        []string
	MaxUpload      string
	Env            bool
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
	if webOpts.Env {
		if err := applyEnvFlags(cmd); err != nil {
			return err
		}
	}
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
		Addr:            addr,
		TLS:             cert,
		Plaintext:       webOpts.NoTLS,
		AllowObfuscated: webOpts.NoTLS,
		AllowCleartext:  webOpts.NoTLS && webOpts.AllowCleartext,
		NoAuth:          webOpts.NoAuth,
		ReadOnly:        webOpts.ReadOnly,
		Title:           webOpts.Title,
		SessionTTL:      time.Duration(webOpts.SessionTTL) * time.Second,
		Sessions:        sessStore,
		TrustedProxies:  trusted,
		MaxUpload:       maxUpload,
		Pads:            padMgr,
		UploadReqs:      uploadReqs,
		PathRefs:        pathRefs,
		AllowedExt:      webOpts.AllowExt,
		Users:           store,
		FS:              fsys,
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

// applyEnvFlags fills every web flag that was NOT given on the command line
// from a NULLOPS_<FLAG> environment variable: the flag name is upper-cased with
// dashes turned into underscores, so --root/-r reads NULLOPS_ROOT, --no-tls
// reads NULLOPS_NO_TLS, --port/-p reads NULLOPS_PORT, --bind/-b reads
// NULLOPS_BIND, and so on for every other flag. Command-line flags take
// precedence; an empty variable is treated as unset. Enabled by --env, which is
// handy for container deployments. Booleans accept 1/t/true/0/f/false; list
// flags (--allow-ext, --trusted-proxy) accept a comma-separated value.
func applyEnvFlags(cmd *cobra.Command) error {
	const prefix = "NULLOPS_"
	var errs []string
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Changed || f.Name == "env" || f.Name == "help" {
			return
		}
		val, ok := os.LookupEnv(prefix + strings.ReplaceAll(strings.ToUpper(f.Name), "-", "_"))
		if !ok || val == "" {
			return
		}
		if err := cmd.Flags().Set(f.Name, val); err != nil {
			errs = append(errs, fmt.Sprintf("%s%s (--%s): %v",
				prefix, strings.ReplaceAll(strings.ToUpper(f.Name), "-", "_"), f.Name, err))
		}
	})
	if len(errs) > 0 {
		return fmt.Errorf("invalid environment value(s): %s", strings.Join(errs, "; "))
	}
	return nil
}

// confirmPlaintext warns that --no-tls drops the browser from AES-GCM down to
// the obfuscated channel (or to cleartext with --allow-cleartext) and asks the
// operator to confirm ([y/N], default no).
func confirmPlaintext() error {
	w := os.Stderr
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  \033[31m!! WARNING: --no-tls\033[0m")
	fmt.Fprintln(w, "  Without TLS the browser does not expose WebCrypto outside localhost, so the")
	fmt.Fprintln(w, "  \033[1mpayload encryption of the /api channel (ECDH + AES-256-GCM) is NOT available.\033[0m")
	if webOpts.AllowCleartext {
		fmt.Fprintln(w, "  With --allow-cleartext the channel falls back to CLEAR TEXT: logins, passwords,")
		fmt.Fprintln(w, "  file names and file contents travel in the clear and can be read or altered by")
		fmt.Fprintln(w, "  anyone on the path (a proxy, IDS/IPS or endpoint agent sees everything).")
	} else {
		fmt.Fprintln(w, "  The channel falls back to OBFUSCATION (keystream XOR) — this is NOT encryption:")
		fmt.Fprintln(w, "  it only keeps payloads out of cleartext against passive inspection and log")
		fmt.Fprintln(w, "  capture. It does NOT protect against an active man-in-the-middle, who without")
		fmt.Fprintln(w, "  TLS can rewrite the delivered code. Non-browser clients still use AES-GCM.")
	}
	fmt.Fprintln(w)
	if webOpts.AssumeYes {
		log.Warn("tls disabled (--no-tls --yes)", "cleartext", webOpts.AllowCleartext)
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
		log.Warn("tls disabled by the operator", "cleartext", webOpts.AllowCleartext)
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
	} else if webOpts.AllowCleartext {
		fmt.Printf("  \033[31mWARNING: TLS disabled (--no-tls --allow-cleartext): the browser channel is\033[0m\n")
		fmt.Printf("  \033[31m         CLEAR TEXT. Credentials, file names and contents are readable.\033[0m\n")
	} else {
		fmt.Printf("  \033[33mtransport: TLS disabled (--no-tls); browser falls back to obfuscation\033[0m\n")
		fmt.Printf("  \033[90m           (keystream XOR, not encryption; no protection against active MITM)\033[0m\n")
	}
	fmt.Printf("  \033[90mpress Ctrl+C to stop\033[0m\n\n")
}

func init() {
	rootCmd.AddCommand(webCmd)
	f := webCmd.Flags()
	f.StringVarP(&webOpts.Root, "root", "r", ".", "Directory to serve")
	f.StringVarP(&webOpts.Bind, "bind", "b", "127.0.0.1", "Bind address (0.0.0.0 for all interfaces)")
	f.IntVarP(&webOpts.Port, "port", "p", 8181, "TCP port")
	f.BoolVar(&webOpts.NoTLS, "no-tls", false, "Serve plain http://; the browser channel falls back to obfuscation (asks for confirmation)")
	f.BoolVar(&webOpts.AllowCleartext, "allow-cleartext", false, "With --no-tls, also offer the raw cleartext channel (no obfuscation)")
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
	f.BoolVar(&webOpts.Env, "env", false, "Read any flag not given on the command line from NULLOPS_<FLAG> env vars (e.g. NULLOPS_ROOT, NULLOPS_BIND, NULLOPS_PORT, NULLOPS_NO_TLS)")
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
