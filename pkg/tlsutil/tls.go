// Package tlsutil generates and loads the HTTPS certificate.
//
// HTTPS is on by default because the /api channel is encrypted with WebCrypto
// (SubtleCrypto), which browsers only expose in a secure context: https://
// pages, or http:// on localhost. Serving over HTTPS (even self-signed) makes
// the UI work from any host on the network.
package tlsutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Options controls certificate generation.
type Options struct {
	Hosts []string // DNS names and/or IPs for the SAN
	Days  int
}

// LocalHosts returns a best-effort list of local hostnames and IPs.
func LocalHosts() []string {
	set := map[string]bool{"localhost": true, "127.0.0.1": true, "::1": true}
	if hn, err := os.Hostname(); err == nil && hn != "" {
		set[hn] = true
	}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLinkLocalUnicast() {
				set[ipn.IP.String()] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for h := range set {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

// Generate writes a new self-signed ECDSA P-256 certificate and key.
func Generate(certFile, keyFile string, opt Options) error {
	if opt.Days <= 0 {
		opt.Days = 825
	}
	if len(opt.Hosts) == 0 {
		opt.Hosts = LocalHosts()
	}
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return err
	}
	cn := "nullops"
	tpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: cn, Organization: []string{"NullOps - Web Resource Manager"}},
		NotBefore:             time.Now().Add(-5 * time.Minute),
		NotAfter:              time.Now().Add(time.Duration(opt.Days) * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	for _, h := range opt.Hosts {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		if ip := net.ParseIP(h); ip != nil {
			tpl.IPAddresses = append(tpl.IPAddresses, ip)
		} else {
			tpl.DNSNames = append(tpl.DNSNames, h)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &priv.PublicKey, priv)
	if err != nil {
		return err
	}
	keyDer, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(certFile), 0o700); err != nil {
		return err
	}
	if err := writePEM(keyFile, "PRIVATE KEY", keyDer, 0o600); err != nil {
		return err
	}
	return writePEM(certFile, "CERTIFICATE", der, 0o644)
}

// Import validates an existing PEM cert/key pair and copies it into place.
func Import(srcCert, srcKey, certFile, keyFile string) error {
	certPEM, err := os.ReadFile(srcCert)
	if err != nil {
		return err
	}
	keyPEM, err := os.ReadFile(srcKey)
	if err != nil {
		return err
	}
	if _, err := tls.X509KeyPair(certPEM, keyPEM); err != nil {
		return fmt.Errorf("certificate and key do not match: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(certFile), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		return err
	}
	return os.WriteFile(certFile, certPEM, 0o644)
}

// Exists reports whether both files exist.
func Exists(certFile, keyFile string) bool {
	_, e1 := os.Stat(certFile)
	_, e2 := os.Stat(keyFile)
	return e1 == nil && e2 == nil
}

// Load loads the key pair.
func Load(certFile, keyFile string) (tls.Certificate, error) {
	return tls.LoadX509KeyPair(certFile, keyFile)
}

// Describe returns subject, SANs, expiry and SHA-256 fingerprint.
func Describe(certFile string) (string, error) {
	data, err := os.ReadFile(certFile)
	if err != nil {
		return "", err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return "", errors.New("no PEM data")
	}
	c, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(c.Raw)
	sans := append([]string{}, c.DNSNames...)
	for _, ip := range c.IPAddresses {
		sans = append(sans, ip.String())
	}
	fp := strings.ToUpper(hex.EncodeToString(sum[:]))
	return fmt.Sprintf("subject=%s sans=[%s] expires=%s sha256=%s",
		c.Subject.CommonName, strings.Join(sans, ", "),
		c.NotAfter.Format("2006-01-02"), fp), nil
}

func writePEM(path, typ string, der []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer f.Close()
	return pem.Encode(f, &pem.Block{Type: typ, Bytes: der})
}
