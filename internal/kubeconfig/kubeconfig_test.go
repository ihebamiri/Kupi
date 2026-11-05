package kubeconfig

import (
	"os"
	"path/filepath"
	"testing"
)

const sample = `apiVersion: v1
kind: Config
current-context: k3d-dev
clusters:
- name: k3d-dev
  cluster:
    server: https://0.0.0.0:6443
- name: eks-prod
  cluster:
    server: https://ABC.gr7.eu-west-1.eks.amazonaws.com
    insecure-skip-tls-verify: true
contexts:
- name: k3d-dev
  context: {cluster: k3d-dev, user: admin@k3d-dev}
- name: prod
  context: {cluster: eks-prod, user: eks-user, namespace: shop}
users:
- name: admin@k3d-dev
  user:
    token: not-a-real-token
- name: eks-user
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1beta1
      command: aws
      args: [eks, get-token, --cluster-name, prod]
      env:
      - name: AWS_PROFILE
        value: secret-profile-value
`

func TestContextsDescribePlugins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(sample), 0o600); err != nil {
		t.Fatal(err)
	}
	loader := NewLoaderForFiles(path)
	contexts, err := loader.Contexts()
	if err != nil {
		t.Fatal(err)
	}
	if len(contexts) != 2 {
		t.Fatalf("got %d contexts", len(contexts))
	}
	dev, prod := contexts[0], contexts[1]
	if dev.Name != "k3d-dev" || !dev.Current || dev.Exec != nil {
		t.Errorf("dev = %+v", dev)
	}
	if prod.Exec == nil || prod.Exec.Command != "aws" || len(prod.Exec.Args) != 4 {
		t.Fatalf("prod exec = %+v", prod.Exec)
	}
	if len(prod.Exec.EnvNames) != 1 || prod.Exec.EnvNames[0] != "AWS_PROFILE" {
		t.Errorf("env names = %v", prod.Exec.EnvNames)
	}
	if !prod.InsecureSkipTLSVerify || prod.Namespace != "shop" {
		t.Errorf("prod = %+v", prod)
	}
	abs, _ := filepath.Abs(path)
	if prod.UserFile != abs {
		t.Errorf("user file = %q, want %q", prod.UserFile, abs)
	}
	if files := loader.Files(); len(files) != 1 || files[0] != abs {
		t.Errorf("files = %v", files)
	}

	h1, _ := FileHash(path)
	_ = os.WriteFile(path, []byte(sample+"\n# edited\n"), 0o600)
	h2, _ := FileHash(path)
	if h1 == "" || h1 == h2 {
		t.Error("hash should change when the file changes")
	}
}
