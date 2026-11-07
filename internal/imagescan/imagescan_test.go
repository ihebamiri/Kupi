package imagescan

import (
	"strings"
	"testing"
)

func TestParseTrivy(t *testing.T) {
	out := []byte(`{"Metadata":{"OS":{"Family":"alpine","Name":"3.19.1"}},"Results":[
	  {"Vulnerabilities":[
	    {"VulnerabilityID":"CVE-2024-0002","PkgName":"zlib","InstalledVersion":"1.3","FixedVersion":"","Severity":"HIGH","Title":"zlib bug"},
	    {"VulnerabilityID":"CVE-2024-0001","PkgName":"openssl","InstalledVersion":"3.1.4","FixedVersion":"3.1.5","Severity":"CRITICAL","Title":"openssl bug","PrimaryURL":"https://avd.aquasec.com/nvd/cve-2024-0001"},
	    {"VulnerabilityID":"CVE-2024-0003","PkgName":"busybox","InstalledVersion":"1.36","FixedVersion":"1.36.1","Severity":"HIGH"},
	    {"VulnerabilityID":"CVE-2024-0003","PkgName":"busybox","InstalledVersion":"1.36","FixedVersion":"1.36.1","Severity":"HIGH"},
	    {"VulnerabilityID":"CVE-2024-0004","PkgName":"musl","InstalledVersion":"1.2","Severity":"weird"}
	  ]},
	  {"Vulnerabilities":null}
	]}`)
	r, err := parseTrivy("app:1", out)
	if err != nil {
		t.Fatal(err)
	}
	if r.OS != "alpine 3.19.1" || r.Counts["CRITICAL"] != 1 || r.Counts["HIGH"] != 2 || r.Counts["UNKNOWN"] != 1 || r.Fixable != 2 {
		t.Errorf("report = %+v", r)
	}
	var ids []string
	for _, f := range r.Findings {
		ids = append(ids, f.ID)
	}
	// Critical first; within a severity, fixable first.
	if got := strings.Join(ids, ","); got != "CVE-2024-0001,CVE-2024-0003,CVE-2024-0002,CVE-2024-0004" {
		t.Errorf("order = %s", got)
	}
	if _, err := parseTrivy("x", []byte("not json")); err == nil {
		t.Error("bad output accepted")
	}
}

func TestParseCosign(t *testing.T) {
	out := []byte(`[
	  {"critical":{"identity":{"docker-reference":"ghcr.io/org/app"}},"optional":{"Issuer":"https://token.actions.githubusercontent.com","Subject":"https://github.com/org/app/.github/workflows/release.yml@refs/tags/v1.0.0"}},
	  {"optional":{"Issuer":"https://token.actions.githubusercontent.com","Subject":"https://github.com/org/app/.github/workflows/release.yml@refs/tags/v1.0.0"}}
	]`)
	r, err := parseCosign("ghcr.io/org/app:1.0.0", out)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Signed || len(r.Signatures) != 1 || !strings.Contains(r.Signatures[0].Identity, "release.yml") {
		t.Errorf("report = %+v", r)
	}
}
