package secrets

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// TLSInfo describes the certificate in a TLS Secret. Certificates are
// public, so all of this is safe to show.
type TLSInfo struct {
	Subject     string   `json:"subject"`
	Issuer      string   `json:"issuer"`
	DNSNames    []string `json:"dnsNames"`
	NotBefore   string   `json:"notBefore"`
	NotAfter    string   `json:"notAfter"`
	DaysLeft    int      `json:"daysLeft"`
	SelfSigned  bool     `json:"selfSigned"`
	IsCA        bool     `json:"isCA"`
	ChainLength int      `json:"chainLength"`
	// KeyStatus is "matches", "mismatch", "missing" or "unreadable".
	KeyStatus string `json:"keyStatus"`
	// Problems are things the user should fix, most serious first.
	Problems []string `json:"problems"`
}

// ExpiryWarningDays is when an expiring certificate becomes a problem.
const ExpiryWarningDays = 30

// CertInfo reads a PEM certificate chain and, if given, checks the private
// key belongs to it. It never fails: anything wrong becomes a problem.
func CertInfo(certPEM, keyPEM []byte, now time.Time) TLSInfo {
	info := TLSInfo{DNSNames: []string{}, Problems: []string{}}
	var chain []*x509.Certificate
	rest := certPEM
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		c, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			info.Problems = append(info.Problems, "A certificate in tls.crt can't be read: "+err.Error())
			continue
		}
		chain = append(chain, c)
	}
	if len(chain) == 0 {
		info.Problems = append(info.Problems, "tls.crt doesn't contain a PEM certificate.")
		info.KeyStatus = "unreadable"
		return info
	}
	leaf := chain[0]
	info.ChainLength = len(chain)
	info.Subject = leaf.Subject.String()
	info.Issuer = leaf.Issuer.String()
	if len(leaf.DNSNames) > 0 {
		info.DNSNames = append(info.DNSNames, leaf.DNSNames...)
	}
	for _, ip := range leaf.IPAddresses {
		info.DNSNames = append(info.DNSNames, ip.String())
	}
	info.NotBefore = leaf.NotBefore.UTC().Format(time.RFC3339)
	info.NotAfter = leaf.NotAfter.UTC().Format(time.RFC3339)
	info.DaysLeft = int(leaf.NotAfter.Sub(now).Hours() / 24)
	info.SelfSigned = bytes.Equal(leaf.RawIssuer, leaf.RawSubject)
	info.IsCA = leaf.IsCA

	switch {
	case now.After(leaf.NotAfter):
		info.Problems = append(info.Problems, fmt.Sprintf("The certificate expired on %s.", leaf.NotAfter.UTC().Format("2 Jan 2006")))
	case now.Before(leaf.NotBefore):
		info.Problems = append(info.Problems, fmt.Sprintf("The certificate isn't valid until %s.", leaf.NotBefore.UTC().Format("2 Jan 2006")))
	case info.DaysLeft < ExpiryWarningDays:
		info.Problems = append(info.Problems, fmt.Sprintf("The certificate expires in %d days.", info.DaysLeft))
	}
	switch k := leaf.PublicKey.(type) {
	case *rsa.PublicKey:
		if k.N.BitLen() < 2048 {
			info.Problems = append(info.Problems, fmt.Sprintf("The certificate uses a weak %d-bit RSA key.", k.N.BitLen()))
		}
	case *ecdsa.PublicKey:
		if k.Curve.Params().BitSize < 256 {
			info.Problems = append(info.Problems, "The certificate uses a weak elliptic curve.")
		}
	}
	switch leaf.SignatureAlgorithm {
	case x509.SHA1WithRSA, x509.ECDSAWithSHA1, x509.MD5WithRSA:
		info.Problems = append(info.Problems, "The certificate is signed with an obsolete algorithm ("+leaf.SignatureAlgorithm.String()+").")
	}
	if len(leaf.DNSNames) == 0 && len(leaf.IPAddresses) == 0 {
		info.Problems = append(info.Problems, "The certificate lists no host names, so browsers and most clients will reject it.")
	}

	switch {
	case len(keyPEM) == 0:
		info.KeyStatus = "missing"
		info.Problems = append(info.Problems, "There is no tls.key, so the certificate can't be served.")
	default:
		if _, err := tls.X509KeyPair(certPEM, keyPEM); err != nil {
			if strings.Contains(err.Error(), "does not match") {
				info.KeyStatus = "mismatch"
				info.Problems = append(info.Problems, "tls.key doesn't belong to this certificate.")
			} else {
				info.KeyStatus = "unreadable"
				info.Problems = append(info.Problems, "tls.key can't be read as a PEM private key.")
			}
		} else {
			info.KeyStatus = "matches"
		}
	}
	return info
}

// CheckPair verifies a certificate and private key belong together before a
// TLS Secret is written.
func CheckPair(certPEM, keyPEM []byte) error {
	if len(certPEM) == 0 || len(keyPEM) == 0 {
		return fmt.Errorf("both the certificate and the private key are required")
	}
	if _, err := tls.X509KeyPair(certPEM, keyPEM); err != nil {
		if strings.Contains(err.Error(), "does not match") {
			return fmt.Errorf("the private key doesn't belong to the certificate")
		}
		return fmt.Errorf("the certificate and key must be PEM: %w", err)
	}
	return nil
}

var registryRe = regexp.MustCompile(`^(https?://)?[a-z0-9]([a-z0-9.-]*[a-z0-9])?(:[0-9]{1,5})?(/[A-Za-z0-9._/-]*)?$`)

// DefaultRegistry is the server kubectl uses for Docker Hub.
const DefaultRegistry = "https://index.docker.io/v1/"

// DockerConfigJSON builds the .dockerconfigjson value for a registry login.
func DockerConfigJSON(server, username, password, email string) ([]byte, error) {
	if server == "" {
		server = DefaultRegistry
	}
	server = strings.ToLower(strings.TrimSpace(server))
	switch {
	case len(server) > 253 || !registryRe.MatchString(server):
		return nil, fmt.Errorf("registry %q isn't a valid host name", server)
	case username == "" || len(username) > 256 || strings.ContainsAny(username, ":\r\n"):
		return nil, fmt.Errorf("the user name is required, up to 256 characters, without ':' or line breaks")
	case password == "" || len(password) > 8192 || strings.ContainsAny(password, "\r\n"):
		return nil, fmt.Errorf("the password or token is required, up to 8192 characters, on one line")
	case len(email) > 256 || strings.ContainsAny(email, "\r\n"):
		return nil, fmt.Errorf("the email address is too long")
	}
	entry := map[string]string{
		"username": username,
		"password": password,
		"auth":     base64.StdEncoding.EncodeToString([]byte(username + ":" + password)),
	}
	if email != "" {
		entry["email"] = email
	}
	return json.Marshal(map[string]interface{}{"auths": map[string]interface{}{server: entry}})
}

// Registries lists the registry servers in a .dockerconfigjson value (or the
// older .dockercfg format), without credentials.
func Registries(value []byte, configJSON bool) []string {
	var servers map[string]json.RawMessage
	if configJSON {
		var doc struct {
			Auths map[string]json.RawMessage `json:"auths"`
		}
		if json.Unmarshal(value, &doc) != nil {
			return []string{}
		}
		servers = doc.Auths
	} else if json.Unmarshal(value, &servers) != nil {
		return []string{}
	}
	out := make([]string, 0, len(servers))
	for s := range servers {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
