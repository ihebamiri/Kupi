package bridge

// ContextInfo describes a kubeconfig context for the UI. It never contains
// credentials.
type ContextInfo struct {
	Name                  string   `json:"name"`
	Cluster               string   `json:"cluster"`
	User                  string   `json:"user"`
	Namespace             string   `json:"namespace"`
	Server                string   `json:"server"`
	Current               bool     `json:"current"`
	Profile               string   `json:"profile"`
	ProfileOverridden     bool     `json:"profileOverridden"`
	DetectedProfile       string   `json:"detectedProfile"`
	File                  string   `json:"file"`
	NeedsTrust            bool     `json:"needsTrust"`
	Trusted               bool     `json:"trusted"`
	ExecCommand           string   `json:"execCommand"`
	ExecArgs              []string `json:"execArgs"`
	ExecEnvNames          []string `json:"execEnvNames"`
	AuthProvider          string   `json:"authProvider"`
	InsecureSkipTLSVerify bool     `json:"insecureSkipTLSVerify"`
	Connected             bool     `json:"connected"`
	ServerVersion         string   `json:"serverVersion"`
	EditingEnabled        bool     `json:"editingEnabled"`
}

// Connection is returned after connecting to a context.
type Connection struct {
	Context          string `json:"context"`
	Profile          string `json:"profile"`
	ServerVersion    string `json:"serverVersion"`
	DefaultNamespace string `json:"defaultNamespace"`
	EditingEnabled   bool   `json:"editingEnabled"`
}

// ResourceRef names one object.
type ResourceRef struct {
	Group     string `json:"group"`
	Version   string `json:"version"`
	Resource  string `json:"resource"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// PlanView is a change preview for the review dialog.
type PlanView struct {
	ID           string   `json:"id"`
	Context      string   `json:"context"`
	Profile      string   `json:"profile"`
	Action       string   `json:"action"`
	APIVersion   string   `json:"apiVersion"`
	Kind         string   `json:"kind"`
	Namespace    string   `json:"namespace"`
	Name         string   `json:"name"`
	Diff         string   `json:"diff"`
	Summary      []string `json:"summary"`
	Warnings     []string `json:"warnings"`
	Conflicts    []string `json:"conflicts"`
	Allowed      bool     `json:"allowed"`
	DeniedReason string   `json:"deniedReason"`
	NeedsReason  bool     `json:"needsReason"`
	ConfirmName  string   `json:"confirmName"`
	ExpiresAt    string   `json:"expiresAt"`
	// Items lists each object when the manifest holds more than one.
	Items []PlanItemView `json:"items"`
}

// PlanItemView is one object of a multi-object apply.
type PlanItemView struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Namespace  string   `json:"namespace"`
	Name       string   `json:"name"`
	State      string   `json:"state"`
	Note       string   `json:"note"`
	Diff       string   `json:"diff"`
	Summary    []string `json:"summary"`
	Warnings   []string `json:"warnings"`
	Conflicts  []string `json:"conflicts"`
}

// CommitResult reports a committed change.
type CommitResult struct {
	Outcome  string `json:"outcome"`
	Message  string `json:"message"`
	AuditSeq int64  `json:"auditSeq"`
}

// AuditEntry is one audit log line for the UI.
type AuditEntry struct {
	Seq      int64  `json:"seq"`
	Time     string `json:"time"`
	Context  string `json:"context"`
	Profile  string `json:"profile"`
	OSUser   string `json:"osUser"`
	KubeUser string `json:"kubeUser"`
	Action   string `json:"action"`
	Resource string `json:"resource"`
	Reason   string `json:"reason"`
	Outcome  string `json:"outcome"`
	Error    string `json:"error"`
	Hash     string `json:"hash"`
}

// AuditVerification reports the integrity of the audit chain.
type AuditVerification struct {
	OK       bool   `json:"ok"`
	Entries  int    `json:"entries"`
	BrokenAt int64  `json:"brokenAt"`
	Problem  string `json:"problem"`
}

// AppInfo describes the running app.
type AppInfo struct {
	Version    string `json:"version"`
	Platform   string `json:"platform"`
	Production bool   `json:"production"`
	ConfigDir  string `json:"configDir"`
}
