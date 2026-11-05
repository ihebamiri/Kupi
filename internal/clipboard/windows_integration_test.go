//go:build windows && integration

package clipboard

import "testing"

// TestWindowsClipboard writes to the real clipboard, reads it back and
// restores what was there.
func TestWindowsClipboard(t *testing.T) {
	b := Native(nil)
	before, ok := b.Text()
	if !ok {
		t.Skip("clipboard busy")
	}
	t.Cleanup(func() { b.SetText(before) })
	if !b.SetText("kupi-clipboard-test ✓") {
		t.Fatal("SetText failed")
	}
	if got, ok := b.Text(); !ok || got != "kupi-clipboard-test ✓" {
		t.Fatalf("Text = %q, %v", got, ok)
	}
	if !b.SetText("") {
		t.Fatal("clearing failed")
	}
	if got, _ := b.Text(); got != "" {
		t.Errorf("not cleared: %q", got)
	}
}
