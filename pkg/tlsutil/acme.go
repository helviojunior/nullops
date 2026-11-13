package tlsutil

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/helviojunior/fmweb/pkg/log"
	"golang.org/x/crypto/acme"
)

// LetsEncryptStagingURL is the Let's Encrypt staging directory (untrusted
// certificates, generous rate limits; use it to test the setup).
const LetsEncryptStagingURL = "https://acme-staging-v02.api.letsencrypt.org/directory"

// DefaultACMEHTTPAddr is where the temporary HTTP-01 responder listens. The CA
// always connects to port 80 of each domain, so a different address only
// makes sense behind a port forward / reverse proxy.
const DefaultACMEHTTPAddr = ":80"

// ACMEOptions controls ObtainACME.
type ACMEOptions struct {
	Domains        []string // DNS names for the certificate (required, no IPs or wildcards)
	Email          string   // optional contact address (mailto:)
	DirectoryURL   string   // ACME directory; overrides Staging when set
	Staging        bool     // use the Let's Encrypt staging directory
	HTTPAddr       string   // listen address for the HTTP-01 responder (default ":80")
	CertFile       string   // destination for the PEM chain (leaf + intermediates)
	KeyFile        string   // destination for the PEM private key
	AccountKeyFile string   // ACME account key (created if missing, reused otherwise)
}

// normalize validates the options and fills in defaults.
func (o *ACMEOptions) normalize() error {
	seen := map[string]bool{}
	var domains []string
	for _, d := range o.Domains {
		d = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(d)), ".")
		if d == "" {
			continue
		}
		if net.ParseIP(d) != nil {
			return fmt.Errorf("%q is an IP address: ACME certificates need DNS names", d)
		}
		if strings.Contains(d, "*") {
			return fmt.Errorf("%q: wildcard certificates need the DNS-01 challenge, which is not supported (HTTP-01 only)", d)
		}
		if !strings.Contains(d, ".") || strings.ContainsAny(d, " /:@") {
			return fmt.Errorf("%q is not a valid public DNS name", d)
		}
		if !seen[d] {
			seen[d] = true
			domains = append(domains, d)
		}
	}
	if len(domains) == 0 {
		return errors.New("at least one domain is required")
	}
	o.Domains = domains
	o.Email = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(o.Email), "mailto:"))
	if o.Email != "" && !strings.Contains(o.Email, "@") {
		return fmt.Errorf("invalid e-mail address %q", o.Email)
	}
	if o.DirectoryURL == "" {
		if o.Staging {
			o.DirectoryURL = LetsEncryptStagingURL
		} else {
			o.DirectoryURL = acme.LetsEncryptURL
		}
	}
	if !strings.HasPrefix(o.DirectoryURL, "https://") && !strings.HasPrefix(o.DirectoryURL, "http://") {
		return fmt.Errorf("invalid ACME directory URL %q", o.DirectoryURL)
	}
	if o.HTTPAddr == "" {
		o.HTTPAddr = DefaultACMEHTTPAddr
	}
	if o.CertFile == "" || o.KeyFile == "" || o.AccountKeyFile == "" {
		return errors.New("certificate, key and account key paths are required")
	}
	return nil
}

// ObtainACME requests a certificate from an ACME CA (Let's Encrypt by default)
// using the HTTP-01 challenge, answered by a temporary HTTP server on
// opt.HTTPAddr. The certificate chain and a new ECDSA P-256 key are written to
// opt.CertFile / opt.KeyFile only after the order succeeds. ctx bounds the
// whole operation.
func ObtainACME(ctx context.Context, opt ACMEOptions) error {
	if err := opt.normalize(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(opt.CertFile), 0o700); err != nil {
		return err
	}

	accountKey, err := loadOrCreateAccountKey(opt.AccountKeyFile)
	if err != nil {
		return err
	}
	client := &acme.Client{Key: accountKey, DirectoryURL: opt.DirectoryURL, UserAgent: "fmweb"}

	// Bind the challenge port first so a busy/privileged port fails fast,
	// before anything is requested from the CA.
	tokens := &challengeTokens{m: map[string]string{}}
	ln, err := net.Listen("tcp", opt.HTTPAddr)
	if err != nil {
		return listenError(opt.HTTPAddr, err)
	}
	srv := &http.Server{Handler: tokens.handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	log.Info("ACME HTTP-01 responder listening", "addr", ln.Addr().String())

	if _, err := client.Discover(ctx); err != nil {
		return fmt.Errorf("cannot reach ACME directory %s: %w", opt.DirectoryURL, err)
	}

	acct := &acme.Account{}
	if opt.Email != "" {
		acct.Contact = []string{"mailto:" + opt.Email}
	}
	if _, err := client.Register(ctx, acct, acme.AcceptTOS); err != nil && !errors.Is(err, acme.ErrAccountAlreadyExists) {
		return fmt.Errorf("ACME account registration failed: %w", err)
	}
	log.Debug("ACME account ready", "directory", opt.DirectoryURL)

	order, err := client.AuthorizeOrder(ctx, acme.DomainIDs(opt.Domains...))
	if err != nil {
		return fmt.Errorf("ACME order failed: %w", err)
	}

	for _, zurl := range order.AuthzURLs {
		if err := authorize(ctx, client, tokens, zurl); err != nil {
			return err
		}
	}

	order, err = client.WaitOrder(ctx, order.URI)
	if err != nil {
		return fmt.Errorf("ACME order did not become ready: %w", err)
	}

	certKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: opt.Domains[0]},
		DNSNames: opt.Domains,
	}, certKey)
	if err != nil {
		return err
	}
	chain, _, err := client.CreateOrderCert(ctx, order.FinalizeURL, csr, true)
	if err != nil {
		return fmt.Errorf("ACME certificate issuance failed: %w", err)
	}
	if len(chain) == 0 {
		return errors.New("ACME server returned an empty certificate chain")
	}

	var certPEM bytes.Buffer
	for _, der := range chain {
		if err := pem.Encode(&certPEM, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
			return err
		}
	}
	keyDer, err := x509.MarshalPKCS8PrivateKey(certKey)
	if err != nil {
		return err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDer})
	if _, err := tls.X509KeyPair(certPEM.Bytes(), keyPEM); err != nil {
		return fmt.Errorf("issued certificate does not match the generated key: %w", err)
	}
	if err := writeFileAtomic(opt.KeyFile, keyPEM, 0o600); err != nil {
		return err
	}
	return writeFileAtomic(opt.CertFile, certPEM.Bytes(), 0o644)
}

// authorize completes one authorization with the HTTP-01 challenge.
func authorize(ctx context.Context, client *acme.Client, tokens *challengeTokens, zurl string) error {
	z, err := client.GetAuthorization(ctx, zurl)
	if err != nil {
		return fmt.Errorf("ACME authorization lookup failed: %w", err)
	}
	if z.Status == acme.StatusValid {
		log.Debug("ACME authorization already valid", "domain", z.Identifier.Value)
		return nil
	}
	var chal *acme.Challenge
	for _, c := range z.Challenges {
		if c.Type == "http-01" {
			chal = c
			break
		}
	}
	if chal == nil {
		return fmt.Errorf("%s: the ACME server offered no http-01 challenge", z.Identifier.Value)
	}
	resp, err := client.HTTP01ChallengeResponse(chal.Token)
	if err != nil {
		return err
	}
	tokens.set(chal.Token, resp)
	defer tokens.del(chal.Token)

	log.Info("validating domain", "domain", z.Identifier.Value)
	if _, err := client.Accept(ctx, chal); err != nil {
		return fmt.Errorf("%s: ACME challenge accept failed: %w", z.Identifier.Value, err)
	}
	if _, err := client.WaitAuthorization(ctx, z.URI); err != nil {
		var ae *acme.AuthorizationError
		if errors.As(err, &ae) {
			return fmt.Errorf("%s: domain validation failed (the domain must resolve to this host "+
				"and port 80 must be reachable from the Internet): %w", z.Identifier.Value, err)
		}
		if ctx.Err() != nil {
			return fmt.Errorf("%s: timed out waiting for domain validation: %w", z.Identifier.Value, err)
		}
		return fmt.Errorf("%s: domain validation failed: %w", z.Identifier.Value, err)
	}
	log.Info("domain validated", "domain", z.Identifier.Value)
	return nil
}

// challengeTokens serves pending HTTP-01 key authorizations.
type challengeTokens struct {
	mu sync.RWMutex
	m  map[string]string
}

func (t *challengeTokens) set(token, resp string) { t.mu.Lock(); t.m[token] = resp; t.mu.Unlock() }
func (t *challengeTokens) del(token string)       { t.mu.Lock(); delete(t.m, token); t.mu.Unlock() }

func (t *challengeTokens) handler() http.Handler {
	const prefix = "/.well-known/acme-challenge/"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, prefix) {
			http.NotFound(w, r)
			return
		}
		token := strings.TrimPrefix(r.URL.Path, prefix)
		t.mu.RLock()
		resp, ok := t.m[token]
		t.mu.RUnlock()
		if !ok {
			log.Debug("ACME challenge request for unknown token", "remote", r.RemoteAddr, "path", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		log.Debug("ACME challenge served", "remote", r.RemoteAddr)
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(resp))
	})
}

func listenError(addr string, err error) error {
	switch {
	case errors.Is(err, syscall.EADDRINUSE):
		return fmt.Errorf("cannot listen on %s for the ACME HTTP-01 challenge: the port is already in use "+
			"(stop the service using it, e.g. a web server, or use --acme-http-addr behind a port forward): %w", addr, err)
	case errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
		return fmt.Errorf("cannot listen on %s for the ACME HTTP-01 challenge: binding ports below 1024 "+
			"requires privileges (run as root, grant CAP_NET_BIND_SERVICE, or use --acme-http-addr behind a port forward): %w", addr, err)
	}
	return fmt.Errorf("cannot listen on %s for the ACME HTTP-01 challenge: %w", addr, err)
}

// loadOrCreateAccountKey reads the ACME account key, or creates and persists
// a new ECDSA P-256 key (PKCS#8 PEM, 0600).
func loadOrCreateAccountKey(path string) (crypto.Signer, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		return parseAccountKey(path, data)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := writePEM(path, "PRIVATE KEY", der, 0o600); err != nil {
		return nil, err
	}
	log.Info("ACME account key created", "path", path)
	return key, nil
}

func parseAccountKey(path string, data []byte) (crypto.Signer, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("%s: no PEM data in ACME account key", path)
	}
	switch block.Type {
	case "EC PRIVATE KEY":
		return x509.ParseECPrivateKey(block.Bytes)
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%s: invalid ACME account key: %w", path, err)
	}
	s, ok := k.(crypto.Signer)
	if !ok {
		return nil, fmt.Errorf("%s: unsupported ACME account key type %T", path, k)
	}
	return s, nil
}

// writeFileAtomic writes data to a temp file in the same folder and renames
// it into place, so a failure never leaves a half-written file.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
