// Package imagescan checks container images with two optional tools:
// Trivy for known vulnerabilities, and cosign for signatures. Both run
// directly (absolute path, argument list, no shell) when installed; Kupi
// adds no scanning library of its own.
package imagescan

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ihebamiri/kupi/internal/procutil"
)

// ErrNotInstalled means the tool isn't on PATH.
var ErrNotInstalled = errors.New("not installed")

const (
	maxOutput   = 32 << 20
	maxFindings = 200
)

// lookPath finds a tool and insists on an absolute path.
func lookPath(name string) (string, error) {
	p, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%s is %w", name, ErrNotInstalled)
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("%s resolved to a relative path", name)
	}
	return p, nil
}

type capped struct {
	buf bytes.Buffer
}

func (c *capped) Write(p []byte) (int, error) {
	if room := maxOutput - c.buf.Len(); room > 0 {
		if len(p) > room {
			c.buf.Write(p[:room])
		} else {
			c.buf.Write(p)
		}
	}
	return len(p), nil
}

func run(ctx context.Context, path string, args ...string) (stdout, stderr []byte, err error) {
	cmd := procutil.Command(ctx, path, args...)
	var out, errb capped
	cmd.Stdout, cmd.Stderr = &out, &errb
	err = cmd.Run()
	if ctx.Err() != nil {
		err = fmt.Errorf("%s took too long", filepath.Base(path))
	}
	return out.buf.Bytes(), errb.buf.Bytes(), err
}

// Finding is one known vulnerability in a package of the image.
type Finding struct {
	ID        string `json:"id"`
	Package   string `json:"package"`
	Installed string `json:"installed"`
	Fixed     string `json:"fixed"`
	Severity  string `json:"severity"`
	Title     string `json:"title"`
	URL       string `json:"url"`
}

// Report is the result of a vulnerability scan.
type Report struct {
	Image   string         `json:"image"`
	Scanner string         `json:"scanner"`
	OS      string         `json:"os"`
	Counts  map[string]int `json:"counts"`
	// Fixable counts findings with a fixed version available.
	Fixable  int       `json:"fixable"`
	Findings []Finding `json:"findings"`
	// Truncated: more findings than shown.
	Truncated bool   `json:"truncated"`
	Scanned   string `json:"scanned"`
}

var severityRank = map[string]int{"CRITICAL": 0, "HIGH": 1, "MEDIUM": 2, "LOW": 3, "UNKNOWN": 4}

// Scan runs Trivy on an image (the local copy when there is one).
func Scan(ctx context.Context, image string) (*Report, error) {
	path, err := lookPath("trivy")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	out, errOut, err := run(ctx, path, "image", "--format", "json", "--quiet", "--scanners", "vuln", "--timeout", "14m", image)
	if err != nil {
		return nil, fmt.Errorf("trivy: %s", lastLine(errOut, err))
	}
	return parseTrivy(image, out)
}

func parseTrivy(image string, out []byte) (*Report, error) {
	var res struct {
		Metadata struct {
			OS struct {
				Family string `json:"Family"`
				Name   string `json:"Name"`
			} `json:"OS"`
		} `json:"Metadata"`
		Results []struct {
			Vulnerabilities []struct {
				VulnerabilityID  string `json:"VulnerabilityID"`
				PkgName          string `json:"PkgName"`
				InstalledVersion string `json:"InstalledVersion"`
				FixedVersion     string `json:"FixedVersion"`
				Severity         string `json:"Severity"`
				Title            string `json:"Title"`
				PrimaryURL       string `json:"PrimaryURL"`
			} `json:"Vulnerabilities"`
		} `json:"Results"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return nil, fmt.Errorf("read trivy's report: %w", err)
	}
	r := &Report{Image: image, Scanner: "Trivy", Counts: map[string]int{}, Findings: []Finding{}, Scanned: time.Now().UTC().Format(time.RFC3339)}
	r.OS = strings.TrimSpace(res.Metadata.OS.Family + " " + res.Metadata.OS.Name)
	seen := map[string]bool{}
	for _, result := range res.Results {
		for _, v := range result.Vulnerabilities {
			key := v.VulnerabilityID + "|" + v.PkgName + "|" + v.InstalledVersion
			if seen[key] {
				continue
			}
			seen[key] = true
			sev := strings.ToUpper(v.Severity)
			if _, ok := severityRank[sev]; !ok {
				sev = "UNKNOWN"
			}
			r.Counts[sev]++
			if v.FixedVersion != "" {
				r.Fixable++
			}
			r.Findings = append(r.Findings, Finding{ID: v.VulnerabilityID, Package: v.PkgName, Installed: v.InstalledVersion, Fixed: v.FixedVersion, Severity: sev, Title: v.Title, URL: v.PrimaryURL})
		}
	}
	sort.SliceStable(r.Findings, func(i, j int) bool {
		a, b := r.Findings[i], r.Findings[j]
		if severityRank[a.Severity] != severityRank[b.Severity] {
			return severityRank[a.Severity] < severityRank[b.Severity]
		}
		if (a.Fixed != "") != (b.Fixed != "") {
			return a.Fixed != ""
		}
		return a.ID < b.ID
	})
	if len(r.Findings) > maxFindings {
		r.Findings, r.Truncated = r.Findings[:maxFindings], true
	}
	return r, nil
}

// Signature is a verified signature on an image.
type Signature struct {
	// Identity is who signed it: an e-mail, or a CI workflow address.
	Identity string `json:"identity"`
	Issuer   string `json:"issuer"`
}

// SignatureReport says whether an image is signed, and by whom.
type SignatureReport struct {
	Image      string      `json:"image"`
	Signed     bool        `json:"signed"`
	Signatures []Signature `json:"signatures"`
	// Problem explains an unsigned or unverifiable image.
	Problem string `json:"problem"`
}

// Verify checks an image's keyless (Sigstore) signatures with cosign. It
// accepts any signer and reports who signed, so the user can judge whether
// that's the publisher they expect. The image must be in a registry.
func Verify(ctx context.Context, image string) (*SignatureReport, error) {
	path, err := lookPath("cosign")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	out, errOut, err := run(ctx, path, "verify", "--output", "json",
		"--certificate-identity-regexp", ".*", "--certificate-oidc-issuer-regexp", ".*", image)
	if err != nil {
		msg := lastLine(errOut, err)
		r := &SignatureReport{Image: image, Signatures: []Signature{}, Problem: msg}
		if strings.Contains(msg, "no matching signatures") || strings.Contains(msg, "no signatures found") {
			r.Problem = "No signature was found for this image."
		}
		return r, nil
	}
	return parseCosign(image, out)
}

func parseCosign(image string, out []byte) (*SignatureReport, error) {
	var sigs []struct {
		Optional map[string]any `json:"optional"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out), &sigs); err != nil {
		return nil, fmt.Errorf("read cosign's answer: %w", err)
	}
	r := &SignatureReport{Image: image, Signed: len(sigs) > 0, Signatures: []Signature{}}
	seen := map[string]bool{}
	for _, s := range sigs {
		id, _ := s.Optional["Subject"].(string)
		iss, _ := s.Optional["Issuer"].(string)
		if seen[id+"|"+iss] {
			continue
		}
		seen[id+"|"+iss] = true
		r.Signatures = append(r.Signatures, Signature{Identity: id, Issuer: iss})
	}
	return r, nil
}

func lastLine(stderr []byte, err error) string {
	lines := strings.Split(strings.TrimSpace(string(stderr)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			if len(l) > 300 {
				l = l[:300]
			}
			return l
		}
	}
	return err.Error()
}

// Tools reports which of Trivy and cosign are installed.
func Tools() map[string]bool {
	out := map[string]bool{}
	for _, t := range []string{"trivy", "cosign"} {
		_, err := lookPath(t)
		out[t] = err == nil
	}
	return out
}
