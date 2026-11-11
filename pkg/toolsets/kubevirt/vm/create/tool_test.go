package create

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"testing"

	"github.com/stretchr/testify/suite"
)

type CreateToolSuite struct {
	suite.Suite
}

func (s *CreateToolSuite) TestParseNetworks() {
	s.Run("nil input returns nil", func() {
		networks, err := parseNetworks(nil)
		s.NoError(err)
		s.Nil(networks)
	})

	s.Run("empty array returns nil", func() {
		networks, err := parseNetworks([]any{})
		s.NoError(err)
		s.Nil(networks)
	})

	s.Run("simple network name as string", func() {
		networks, err := parseNetworks([]any{"vlan-network"})
		s.NoError(err)
		s.Require().Len(networks, 1)
		s.Equal("vlan-network", networks[0].Name)
		s.Equal("vlan-network", networks[0].NetworkName)
	})

	s.Run("multiple network names as strings", func() {
		networks, err := parseNetworks([]any{"vlan-network", "storage-network"})
		s.NoError(err)
		s.Require().Len(networks, 2)
		s.Equal("vlan-network", networks[0].Name)
		s.Equal("vlan-network", networks[0].NetworkName)
		s.Equal("storage-network", networks[1].Name)
		s.Equal("storage-network", networks[1].NetworkName)
	})

	s.Run("empty strings are skipped", func() {
		networks, err := parseNetworks([]any{"vlan-network", "", "storage-network"})
		s.NoError(err)
		s.Require().Len(networks, 2)
		s.Equal("vlan-network", networks[0].Name)
		s.Equal("storage-network", networks[1].Name)
	})

	s.Run("object with name and networkName", func() {
		networks, err := parseNetworks([]any{
			map[string]any{"name": "vlan100", "networkName": "vlan-network"},
		})
		s.NoError(err)
		s.Require().Len(networks, 1)
		s.Equal("vlan100", networks[0].Name)
		s.Equal("vlan-network", networks[0].NetworkName)
	})

	s.Run("multiple objects", func() {
		networks, err := parseNetworks([]any{
			map[string]any{"name": "vlan100", "networkName": "vlan-network"},
			map[string]any{"name": "storage", "networkName": "storage-network"},
		})
		s.NoError(err)
		s.Require().Len(networks, 2)
		s.Equal("vlan100", networks[0].Name)
		s.Equal("vlan-network", networks[0].NetworkName)
		s.Equal("storage", networks[1].Name)
		s.Equal("storage-network", networks[1].NetworkName)
	})

	s.Run("object with only networkName uses it as name", func() {
		networks, err := parseNetworks([]any{
			map[string]any{"networkName": "vlan-network"},
		})
		s.NoError(err)
		s.Require().Len(networks, 1)
		s.Equal("vlan-network", networks[0].Name)
		s.Equal("vlan-network", networks[0].NetworkName)
	})

	s.Run("object missing networkName returns error", func() {
		_, err := parseNetworks([]any{
			map[string]any{"name": "vlan100"},
		})
		s.Error(err)
		s.Contains(err.Error(), "missing required 'networkName' field")
	})

	s.Run("mixed strings and objects", func() {
		networks, err := parseNetworks([]any{
			"simple-network",
			map[string]any{"name": "vlan100", "networkName": "vlan-network"},
		})
		s.NoError(err)
		s.Require().Len(networks, 2)
		s.Equal("simple-network", networks[0].Name)
		s.Equal("simple-network", networks[0].NetworkName)
		s.Equal("vlan100", networks[1].Name)
		s.Equal("vlan-network", networks[1].NetworkName)
	})

	s.Run("invalid type returns error", func() {
		_, err := parseNetworks([]any{123})
		s.Error(err)
		s.Contains(err.Error(), "invalid type")
	})
}

func (s *CreateToolSuite) TestBuildVM() {
	field := func(obj *unstructured.Unstructured, path ...string) any {
		v, _, _ := unstructured.NestedFieldNoCopy(obj.Object, path...)
		return v
	}
	s.Run("VM without networks", func() {
		vm := buildVM(vmParams{
			Namespace:     "test-ns",
			Name:          "test-vm",
			ContainerDisk: "quay.io/containerdisks/fedora:latest",
			RunStrategy:   "Halted",
		})
		s.Equal("VirtualMachine", vm.GetKind())
		s.Equal("kubevirt.io/v1", vm.GetAPIVersion())
		s.Equal("test-vm", vm.GetName())
		s.Equal("test-ns", vm.GetNamespace())
		s.Equal("Halted", field(vm, "spec", "runStrategy"))
		s.Equal([]any{map[string]any{"name": "test-vm-rootdisk", "containerDisk": map[string]any{"image": "quay.io/containerdisks/fedora:latest"}}},
			field(vm, "spec", "template", "spec", "volumes"))
		s.Equal("2Gi", field(vm, "spec", "template", "spec", "domain", "memory", "guest"))
		s.Nil(field(vm, "spec", "template", "spec", "networks"))
		s.Nil(field(vm, "spec", "template", "spec", "domain", "devices", "interfaces"))
	})

	s.Run("VM with instancetype, preference and DataSource", func() {
		vm := buildVM(vmParams{
			Namespace: "test-ns", Name: "test-vm", RunStrategy: "Always", Storage: "50Gi",
			Instancetype: "u1.medium", InstancetypeKind: "VirtualMachineClusterInstancetype",
			Preference: "fedora", PreferenceKind: "VirtualMachineClusterPreference",
			UseDataSource: true, DataSourceName: "fedora", DataSourceNamespace: "os-images",
		})
		s.Equal(map[string]any{"name": "u1.medium", "kind": "VirtualMachineClusterInstancetype"}, field(vm, "spec", "instancetype"))
		s.Equal(map[string]any{"name": "fedora", "kind": "VirtualMachineClusterPreference"}, field(vm, "spec", "preference"))
		s.Nil(field(vm, "spec", "template", "spec", "domain", "memory"), "an instancetype sets the memory")
		s.Equal("50Gi", field(vm, "spec", "dataVolumeTemplates").([]any)[0].(map[string]any)["spec"].(map[string]any)["storage"].(map[string]any)["resources"].(map[string]any)["requests"].(map[string]any)["storage"])
		s.Equal(map[string]any{"name": "test-vm-rootdisk", "dataVolume": map[string]any{"name": "test-vm-rootdisk"}},
			field(vm, "spec", "template", "spec", "volumes").([]any)[0])
	})

	s.Run("VM with networks", func() {
		vm := buildVM(vmParams{
			Namespace:     "test-ns",
			Name:          "test-vm",
			ContainerDisk: "quay.io/containerdisks/fedora:latest",
			RunStrategy:   "Halted",
			Networks: []NetworkConfig{
				{Name: "vlan100", NetworkName: "vlan-network"},
				{Name: "storage", NetworkName: "storage-network"},
			},
		})
		s.Equal([]any{
			map[string]any{"name": "vlan100", "bridge": map[string]any{}},
			map[string]any{"name": "storage", "bridge": map[string]any{}},
		}, field(vm, "spec", "template", "spec", "domain", "devices", "interfaces"))
		s.Equal([]any{
			map[string]any{"name": "vlan100", "multus": map[string]any{"networkName": "vlan-network"}},
			map[string]any{"name": "storage", "multus": map[string]any{"networkName": "storage-network"}},
		}, field(vm, "spec", "template", "spec", "networks"))
	})

	s.Run("argument text stays a field value", func() {
		value := "quay.io/x:1\n---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: extra\n"
		vm := buildVM(vmParams{Namespace: "test-ns", Name: "test-vm", ContainerDisk: value, RunStrategy: "Halted"})
		s.Equal(value, field(vm, "spec", "template", "spec", "volumes").([]any)[0].(map[string]any)["containerDisk"].(map[string]any)["image"])
		s.Equal("VirtualMachine", vm.GetKind())
	})
}

func TestCreateToolSuite(t *testing.T) {
	suite.Run(t, new(CreateToolSuite))
}
