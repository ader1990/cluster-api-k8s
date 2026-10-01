//go:build e2e
// +build e2e

/*
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/cluster-api/test/framework/clusterctl"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/yaml"
)

// Test suite constants for e2e config variables.
const (
	KubernetesVersionManagement     = "KUBERNETES_VERSION_MANAGEMENT"
	KubernetesVersion               = "KUBERNETES_VERSION"
	KubernetesVersionUpgradeTo      = "KUBERNETES_VERSION_UPGRADE_TO"
	CPMachineTemplateUpgradeTo      = "CONTROL_PLANE_MACHINE_TEMPLATE_UPGRADE_TO"
	WorkersMachineTemplateUpgradeTo = "WORKERS_MACHINE_TEMPLATE_UPGRADE_TO"
	IPFamily                        = "IP_FAMILY"
	InPlaceUpgradeOption            = "IN_PLACE_UPGRADE_OPTION"
)

func Byf(format string, a ...interface{}) {
	By(fmt.Sprintf(format, a...))
}

func setupSpecNamespace(ctx context.Context, specName string, clusterProxy framework.ClusterProxy, artifactFolder string) (*corev1.Namespace, context.CancelFunc) {
	Byf("Creating a namespace for hosting the %q test spec", specName)
	namespace, cancelWatches := framework.CreateNamespaceAndWatchEvents(ctx, framework.CreateNamespaceAndWatchEventsInput{
		Creator:   clusterProxy.GetClient(),
		ClientSet: clusterProxy.GetClientSet(),
		Name:      fmt.Sprintf("%s-%s", specName, util.RandomString(6)),
		LogFolder: filepath.Join(artifactFolder, "clusters", clusterProxy.GetName()),
	})

	return namespace, cancelWatches
}

type cleanupInput struct {
	SpecName             string
	ClusterProxy         framework.ClusterProxy
	ArtifactFolder       string
	ClusterctlConfigPath string
	Namespace            *corev1.Namespace
	CancelWatches        context.CancelFunc
	Cluster              *clusterv1.Cluster
	IntervalsGetter      func(spec, key string) []interface{}
	SkipCleanup          bool
	AdditionalCleanup    func()
}

func dumpSpecResourcesAndCleanup(ctx context.Context, input cleanupInput) {
	defer func() {
		input.CancelWatches()
	}()

	if input.Cluster == nil {
		By("Unable to dump workload cluster logs as the cluster is nil")
	} else {
		Byf("Dumping logs from the %q workload cluster", input.Cluster.Name)
		input.ClusterProxy.CollectWorkloadClusterLogs(ctx, input.Cluster.Namespace, input.Cluster.Name, filepath.Join(input.ArtifactFolder, "clusters", input.Cluster.Name))
	}

	Byf("Dumping all the Cluster API resources in the %q namespace", input.Namespace.Name)
	// Dump all Cluster API related resources to artifacts before deleting them.
	framework.DumpAllResources(ctx, framework.DumpAllResourcesInput{
		Lister:               input.ClusterProxy.GetClient(),
		Namespace:            input.Namespace.Name,
		LogPath:              filepath.Join(input.ArtifactFolder, "clusters", input.ClusterProxy.GetName(), "resources"),
		KubeConfigPath:       input.ClusterProxy.GetKubeconfigPath(),
		ClusterctlConfigPath: input.ClusterctlConfigPath,
	})

	if input.SkipCleanup {
		return
	}

	Byf("Deleting all clusters in the %s namespace", input.Namespace.Name)
	// While https://github.com/kubernetes-sigs/cluster-api/issues/2955 is addressed in future iterations, there is a chance
	// that cluster variable is not set even if the cluster exists, so we are calling DeleteAllClustersAndWait
	// instead of DeleteClusterAndWait
	framework.DeleteAllClustersAndWait(ctx, framework.DeleteAllClustersAndWaitInput{
		ClusterProxy:         input.ClusterProxy,
		Namespace:            input.Namespace.Name,
		ClusterctlConfigPath: input.ClusterctlConfigPath,
	}, input.IntervalsGetter(input.SpecName, "wait-delete-cluster")...)

	Byf("Deleting namespace used for hosting the %q test spec", input.SpecName)
	framework.DeleteNamespace(ctx, framework.DeleteNamespaceInput{
		Deleter: input.ClusterProxy.GetClient(),
		Name:    input.Namespace.Name,
	})

	if input.AdditionalCleanup != nil {
		Byf("Running additional cleanup for the %q test spec", input.SpecName)
		input.AdditionalCleanup()
	}
}

func localLoadE2EConfig(configPath string) *clusterctl.E2EConfig {
	configData, err := os.ReadFile(configPath) //nolint:gosec
	Expect(err).ToNot(HaveOccurred(), "Failed to read the e2e test config file")
	Expect(configData).ToNot(BeEmpty(), "The e2e test config file should not be empty")

	config := &clusterctl.E2EConfig{}
	Expect(yaml.Unmarshal(configData, config)).To(Succeed(), "Failed to convert the e2e test config file to yaml")

	config.Defaults()
	config.AbsPaths(filepath.Dir(configPath))

	// TODO: this is the reason why we can't use this at present for the RKE2 tests
	// Expect(config.Validate()).To(Succeed(), "The e2e test config file is not valid")

	return config
}

// createLXCSecret creates the LXC secret for LXD provider if needed
func createLXCSecret(ctx context.Context, clusterProxy framework.ClusterProxy, e2eConfig *clusterctl.E2EConfig, namespace string) {
	if !slices.Contains(e2eConfig.InfrastructureProviders(), "incus") {
		return
	}

	By("Creating LXC secret for LXD provider")

	// Get values from environment variables with defaults
	homeDir, err := os.UserHomeDir()
	Expect(err).ToNot(HaveOccurred(), "Failed to get user home directory")

	// Environment variables for LXD configuration
	lxdAddress, err := getLXDDefaultAddress()
	Expect(err).ToNot(HaveOccurred(), "Failed to get LXD default address")

	remote := getEnvWithDefault("LXD_REMOTE", lxdAddress)
	serverCertPath := getEnvWithDefault("LXD_SERVER_CERT", filepath.Join(homeDir, "snap/lxd/common/config/servercerts/local-https.crt"))
	clientCertPath := getEnvWithDefault("LXD_CLIENT_CERT", filepath.Join(homeDir, "snap/lxd/common/config/client.crt"))
	clientKeyPath := getEnvWithDefault("LXD_CLIENT_KEY", filepath.Join(homeDir, "snap/lxd/common/config/client.key"))
	project := getEnvWithDefault("LXD_PROJECT", "default")

	createLXCSecretInNamespace(ctx, clusterProxy, namespace, remote, serverCertPath, clientCertPath, clientKeyPath, project)
}

func getEnvWithDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getLXDDefaultAddress gets the LXD HTTPS address from lxc config
func getLXDDefaultAddress() (string, error) {
	cmd := exec.Command("lxc", "config", "get", "core.https_address")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get LXD address: %w", err)
	}

	address := strings.TrimSpace(string(output))
	if address == "" {
		return "", fmt.Errorf("LXD core.https_address is empty")
	}

	// Ensure it has https:// prefix
	if !strings.HasPrefix(address, "https://") {
		address = "https://" + address
	}

	return address, nil
}

func createLXCSecretInNamespace(ctx context.Context, clusterProxy framework.ClusterProxy, namespace, remote, serverCertPath, clientCertPath, clientKeyPath, project string) {
	clientset := clusterProxy.GetClientSet()

	// Read certificate files
	serverCrt, err := os.ReadFile(serverCertPath)
	if err != nil {
		fmt.Printf("Warning: Failed to read server certificate from %s: %v\n", serverCertPath, err)
		serverCrt = []byte{}
	}

	clientCrt, err := os.ReadFile(clientCertPath)
	if err != nil {
		fmt.Printf("Warning: Failed to read client certificate from %s: %v\n", clientCertPath, err)
		clientCrt = []byte{}
	}

	clientKey, err := os.ReadFile(clientKeyPath)
	if err != nil {
		fmt.Printf("Warning: Failed to read client key from %s: %v\n", clientKeyPath, err)
		clientKey = []byte{}
	}

	// Create the secret
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "lxc-secret",
			Namespace: namespace,
		},
		StringData: map[string]string{
			"server":     remote,
			"server-crt": string(serverCrt),
			"client-crt": string(clientCrt),
			"client-key": string(clientKey),
			"project":    project,
		},
	}

	_, err = clientset.CoreV1().Secrets(namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		// If secret already exists, update it
		_, err = clientset.CoreV1().Secrets(namespace).Update(ctx, secret, metav1.UpdateOptions{})
		Expect(err).ToNot(HaveOccurred(), "Failed to create or update LXC secret")
	}

	fmt.Printf("Created LXC secret with server: %s in namespace: %s\n", remote, namespace)
}

type UpgradeManagementClusterAndWaitInput struct {
	ClusterProxy              framework.ClusterProxy
	ClusterctlConfigPath      string
	ClusterctlVariables       map[string]string
	Contract                  string
	CoreProvider              string
	BootstrapProviders        []string
	ControlPlaneProviders     []string
	InfrastructureProviders   []string
	IPAMProviders             []string
	RuntimeExtensionProviders []string
	AddonProviders            []string
	LogFolder                 string
	ClusterctlBinaryPath      string
}

// UpgradeManagementClusterAndWait upgrades provider a management cluster using clusterctl, and waits for the cluster to be ready.
func UpgradeManagementClusterAndWait(ctx context.Context, input UpgradeManagementClusterAndWaitInput, intervals ...interface{}) {
	Expect(ctx).NotTo(BeNil(), "ctx is required for UpgradeManagementClusterAndWait")
	Expect(input.ClusterProxy).ToNot(BeNil(), "Invalid argument. input.ClusterProxy can't be nil when calling UpgradeManagementClusterAndWait")
	Expect(input.ClusterctlConfigPath).To(BeAnExistingFile(), "Invalid argument. input.ClusterctlConfigPath must be an existing file when calling UpgradeManagementClusterAndWait")
	// Check if the user want a custom upgrade
	isCustomUpgrade := input.CoreProvider != "" ||
		len(input.BootstrapProviders) > 0 ||
		len(input.ControlPlaneProviders) > 0 ||
		len(input.InfrastructureProviders) > 0 ||
		len(input.IPAMProviders) > 0 ||
		len(input.RuntimeExtensionProviders) > 0 ||
		len(input.AddonProviders) > 0

	Expect((input.Contract != "" && !isCustomUpgrade) || (input.Contract == "" && isCustomUpgrade)).To(BeTrue(), `Invalid argument. Either the input.Contract parameter or at least one of the following providers has to be set:
		input.CoreProvider, input.BootstrapProviders, input.ControlPlaneProviders, input.InfrastructureProviders, input.IPAMProviders, input.RuntimeExtensionProviders, input.AddonProviders`)

	Expect(os.MkdirAll(input.LogFolder, 0750)).To(Succeed(), "Invalid argument. input.LogFolder can't be created for UpgradeManagementClusterAndWait")

	upgradeInput := clusterctl.UpgradeInput{
		ClusterctlConfigPath:      input.ClusterctlConfigPath,
		ClusterctlVariables:       input.ClusterctlVariables,
		ClusterName:               input.ClusterProxy.GetName(),
		KubeconfigPath:            input.ClusterProxy.GetKubeconfigPath(),
		Contract:                  input.Contract,
		CoreProvider:              input.CoreProvider,
		BootstrapProviders:        input.BootstrapProviders,
		ControlPlaneProviders:     input.ControlPlaneProviders,
		InfrastructureProviders:   input.InfrastructureProviders,
		IPAMProviders:             input.IPAMProviders,
		RuntimeExtensionProviders: input.RuntimeExtensionProviders,
		AddonProviders:            input.AddonProviders,
		LogFolder:                 input.LogFolder,
	}

	client := input.ClusterProxy.GetClient()

	if input.ClusterctlBinaryPath != "" {
		Expect(clusterctl.UpgradeWithBinary(ctx, input.ClusterctlBinaryPath, upgradeInput)).To(Succeed())
	} else {
		clusterctl.Upgrade(ctx, upgradeInput)
	}

	By("Waiting for provider controllers to be running")
	controllersDeployments := framework.GetControllerDeployments(ctx, framework.GetControllerDeploymentsInput{
		Lister: client,
		// This namespace has been dropped in v0.4.x.
		// We have to exclude this namespace here as after an upgrade from v0.3x there won't
		// be a controller in this namespace anymore and if we wait for it to come up the test would fail.
		// Note: We can drop this as soon as we don't have a test upgrading from v0.3.x anymore.
		ExcludeNamespaces: []string{"capi-webhook-system"},
	})
	Expect(controllersDeployments).ToNot(BeEmpty(), "The list of controller deployments should not be empty")
	for _, deployment := range controllersDeployments {
		framework.WaitForDeploymentsAvailable(ctx, framework.WaitForDeploymentsAvailableInput{
			Getter:     client,
			Deployment: deployment,
		}, intervals...)

		framework.WatchPodMetrics(ctx, framework.WatchPodMetricsInput{
			GetLister:   client,
			ClientSet:   input.ClusterProxy.GetClientSet(),
			Deployment:  deployment,
			MetricsPath: filepath.Join(input.LogFolder, "metrics", deployment.GetNamespace()),
		})
	}
}
