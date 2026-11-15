package core

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/containers/kubernetes-mcp-server/pkg/kubernetes"
)

type ReceiptSuite struct {
	suite.Suite
}

func configMap(rv string, data map[string]any, labels map[string]any) *unstructured.Unstructured {
	metadata := map[string]any{"name": "cm", "namespace": "default", "uid": "u-1", "resourceVersion": rv}
	if labels != nil {
		metadata["labels"] = labels
	}
	obj := map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": metadata}
	if data != nil {
		obj["data"] = data
	}
	return &unstructured.Unstructured{Object: obj}
}

func (s *ReceiptSuite) TestUpdateListsOnlyTheChangedFields() {
	before := configMap("1", map[string]any{"a": "1", "b": "2", "gone": "x"}, map[string]any{"app.kubernetes.io/name": "web"})
	after := configMap("2", map[string]any{"a": "1", "b": "3", "new.key": "n"}, map[string]any{"app.kubernetes.io/name": "web"})
	receipt := receiptWithChanges(kubernetes.WriteRecord{Previous: before, PreviousKnown: true, Result: after})
	s.Equal(map[string]any{"data.b": "3", `data["new.key"]`: "n", "data.gone": nil}, receipt["changed"])
	s.NotContains(receipt, "created")
	s.Equal("2", receipt["metadata"].(map[string]any)["resourceVersion"], "the receipt keeps the new version")
}

func (s *ReceiptSuite) TestUnchangedApplyListsNoField() {
	obj := configMap("1", map[string]any{"a": "1"}, nil)
	receipt := receiptWithChanges(kubernetes.WriteRecord{Previous: obj, PreviousKnown: true, Result: obj.DeepCopy()})
	s.Equal(map[string]any{}, receipt["changed"])
}

func (s *ReceiptSuite) TestCreateListsTheSentFieldsWithStoredValues() {
	sent := configMap("", map[string]any{"a": "1"}, map[string]any{"tier": "x"})
	stored := configMap("7", map[string]any{"a": "1"}, map[string]any{"tier": "x"})
	stored.Object["metadata"].(map[string]any)["managedFields"] = []any{map[string]any{"manager": "m"}}
	receipt := receiptWithChanges(kubernetes.WriteRecord{Sent: sent, PreviousKnown: true, Result: stored})
	s.Equal(true, receipt["created"])
	s.Equal(map[string]any{"data.a": "1", "metadata.labels.tier": "x"}, receipt["changed"])
}

func (s *ReceiptSuite) TestUnknownEarlierStateListsTheSentFieldsAsSet() {
	sent := configMap("", map[string]any{"a": "1"}, nil)
	receipt := receiptWithChanges(kubernetes.WriteRecord{Sent: sent, Result: configMap("3", map[string]any{"a": "1", "b": "2"}, nil)})
	s.Equal(map[string]any{"data.a": "1"}, receipt["set"])
	s.NotContains(receipt, "changed")
}

func (s *ReceiptSuite) TestListsAreComparedByIndex() {
	before := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{"containers": []any{
		map[string]any{"name": "app", "image": "nginx:1"}, map[string]any{"name": "side", "image": "busybox"},
	}}}}
	after := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{"containers": []any{
		map[string]any{"name": "app", "image": "nginx:2"},
	}}}}
	receipt := receiptWithChanges(kubernetes.WriteRecord{Previous: before, PreviousKnown: true, Result: after})
	s.Equal(map[string]any{"spec.containers[0].image": "nginx:2", "spec.containers[1]": nil}, receipt["changed"])
}

func (s *ReceiptSuite) TestStatusAndServerMetadataAreLeftOut() {
	before := configMap("1", map[string]any{"a": "1"}, nil)
	after := configMap("2", map[string]any{"a": "1"}, nil)
	after.Object["status"] = map[string]any{"phase": "Ready"}
	after.Object["metadata"].(map[string]any)["generation"] = int64(2)
	receipt := receiptWithChanges(kubernetes.WriteRecord{Previous: before, PreviousKnown: true, Result: after})
	s.Equal(map[string]any{}, receipt["changed"])
}

func (s *ReceiptSuite) TestLongValuesAndManyFieldsStayCompact() {
	data := map[string]any{"big": strings.Repeat("x", 1000)}
	for i := 0; i < 60; i++ {
		data[fmt.Sprintf("k%03d", i)] = "v"
	}
	receipt := receiptWithChanges(kubernetes.WriteRecord{Sent: configMap("", data, nil), PreviousKnown: true, Result: configMap("1", data, nil)})
	changed := receipt["changed"].(map[string]any)
	s.Len(changed, receiptMaxFields)
	s.Equal(61-receiptMaxFields, receipt["changedNotShown"])
	big := changed["data.big"].(string)
	s.True(strings.HasPrefix(big, strings.Repeat("x", receiptMaxValue)+"…"))
	s.Contains(big, "(1000 characters in all)")
}

func (s *ReceiptSuite) TestOutputModes() {
	before := configMap("1", map[string]any{"a": "1"}, nil)
	after := configMap("2", map[string]any{"a": "2"}, nil)
	records := []kubernetes.WriteRecord{{Previous: before, PreviousKnown: true, Result: after}}
	receipt, err := writeResultYaml(records, outputReceipt)
	s.Require().NoError(err)
	s.Contains(receipt, "data.a: \"2\"")
	compact, err := writeResultYaml(records, outputCompact)
	s.Require().NoError(err)
	s.NotContains(compact, "data.a")
	s.NotContains(compact, "changed")
	full, err := writeResultYaml(records, outputFull)
	s.Require().NoError(err)
	s.Contains(full, "a: \"2\"")
	s.NotContains(full, "changed")
}

func TestReceipt(t *testing.T) {
	suite.Run(t, new(ReceiptSuite))
}
