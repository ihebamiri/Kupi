package schema

import (
	"errors"
	"testing"
)

// fixture mimics the shape of a Kubernetes OpenAPI v3 document.
const fixture = `{
  "components": {"schemas": {
    "io.example.v1.Widget": {
      "type": "object",
      "x-kubernetes-group-version-kind": [{"group": "example.io", "version": "v1", "kind": "Widget"}],
      "properties": {
        "apiVersion": {"type": "string"},
        "kind": {"type": "string"},
        "metadata": {"allOf": [{"$ref": "#/components/schemas/io.k8s.apimachinery.pkg.apis.meta.v1.ObjectMeta"}], "default": {}},
        "spec": {"allOf": [{"$ref": "#/components/schemas/io.example.v1.WidgetSpec"}], "description": "Desired state."},
        "status": {"type": "object", "properties": {"ready": {"type": "boolean"}}}
      }
    },
    "io.example.v1.WidgetSpec": {
      "type": "object",
      "required": ["size"],
      "properties": {
        "size": {"type": "integer", "format": "int32"},
        "color": {"type": "string", "enum": ["red", "blue"]},
        "port": {"allOf": [{"$ref": "#/components/schemas/io.k8s.apimachinery.pkg.util.intstr.IntOrString"}]},
        "limits": {"type": "object", "additionalProperties": {"allOf": [{"$ref": "#/components/schemas/io.k8s.apimachinery.pkg.api.resource.Quantity"}]}},
        "tags": {"type": "array", "items": {"type": "string"}},
        "child": {"allOf": [{"$ref": "#/components/schemas/io.example.v1.WidgetSpec"}]},
        "extra": {"type": "object", "x-kubernetes-preserve-unknown-fields": true}
      }
    },
    "io.k8s.apimachinery.pkg.apis.meta.v1.ObjectMeta": {
      "type": "object",
      "properties": {
        "name": {"type": "string"},
        "namespace": {"type": "string"},
        "labels": {"type": "object", "additionalProperties": {"type": "string"}},
        "uid": {"type": "string"},
        "managedFields": {"type": "array", "items": {"type": "object"}}
      }
    },
    "io.k8s.apimachinery.pkg.util.intstr.IntOrString": {"type": "string", "format": "int-or-string"},
    "io.k8s.apimachinery.pkg.api.resource.Quantity": {"type": "string"}
  }}
}`

func field(t *testing.T, n *Node, name string) *Field {
	t.Helper()
	for i := range n.Fields {
		if n.Fields[i].Name == name {
			return &n.Fields[i]
		}
	}
	t.Fatalf("field %q not found", name)
	return nil
}

func TestBuild(t *testing.T) {
	root, err := Build([]byte(fixture), "example.io", "v1", "Widget")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range root.Fields {
		if f.Name == "status" {
			t.Error("status should be dropped")
		}
	}
	meta := field(t, root, "metadata").Node
	if len(meta.Fields) != 3 {
		t.Errorf("metadata fields = %v, want name, namespace, labels only", meta.Fields)
	}
	if field(t, meta, "labels").Node.Type != "map" {
		t.Error("labels should be a map")
	}

	spec := field(t, root, "spec")
	if spec.Node.Description != "Desired state." {
		t.Errorf("spec description = %q", spec.Node.Description)
	}
	if spec.Node.Fields[0].Name != "size" || !spec.Node.Fields[0].Required {
		t.Error("required fields should come first")
	}
	checks := map[string]string{"size": "integer", "color": "string", "port": "intOrString", "limits": "map", "tags": "array", "extra": "any"}
	for name, want := range checks {
		if got := field(t, spec.Node, name).Node.Type; got != want {
			t.Errorf("%s type = %s, want %s", name, got, want)
		}
	}
	if field(t, spec.Node, "limits").Node.Values.Type != "quantity" {
		t.Error("limits values should be quantities")
	}
	if enum := field(t, spec.Node, "color").Node.Enum; len(enum) != 2 {
		t.Errorf("enum = %v", enum)
	}
	if child := field(t, spec.Node, "child").Node; !child.Truncated {
		t.Error("recursive reference should be truncated")
	}
}

func TestBuildUnknownKind(t *testing.T) {
	if _, err := Build([]byte(fixture), "example.io", "v1", "Gadget"); !errors.Is(err, ErrKindNotFound) {
		t.Errorf("got %v, want ErrKindNotFound", err)
	}
}
