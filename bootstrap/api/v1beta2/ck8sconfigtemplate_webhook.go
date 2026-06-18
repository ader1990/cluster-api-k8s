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

package v1beta2

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// SetupWebhookWithManager will setup the webhooks for the CK8sControlPlane.
func (c *CK8sConfigTemplate) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &CK8sConfigTemplate{}).
		WithValidator(c).
		Complete()
}

// +kubebuilder:webhook:verbs=create;update,path=/validate-bootstrap-cluster-x-k8s-io-v1beta2-ck8sconfigtemplate,mutating=false,failurePolicy=fail,matchPolicy=Equivalent,groups=bootstrap.cluster.x-k8s.io,resources=ck8sconfigtemplates,versions=v1beta2,name=validation.ck8sconfigtemplate.bootstrap.cluster.x-k8s.io,sideEffects=None,admissionReviewVersions=v1;v1beta2
// +kubebuilder:webhook:verbs=create;update,path=/mutate-bootstrap-cluster-x-k8s-io-v1beta2-ck8sconfigtemplate,mutating=true,failurePolicy=fail,matchPolicy=Equivalent,groups=bootstrap.cluster.x-k8s.io,resources=ck8sconfigtemplates,versions=v1beta2,name=default.ck8sconfigtemplate.bootstrap.cluster.x-k8s.io,sideEffects=None,admissionReviewVersions=v1;v1beta2

var _ admission.Validator[*CK8sConfigTemplate] = &CK8sConfigTemplate{}

// ValidateCreate will do any extra validation when creating a CK8sControlPlane.
func (c *CK8sConfigTemplate) ValidateCreate(_ context.Context, _ *CK8sConfigTemplate) (admission.Warnings, error) {
	return []string{}, nil
}

func (c *CK8sConfigTemplate) ValidateUpdate(_ context.Context, _ *CK8sConfigTemplate, _ *CK8sConfigTemplate) (admission.Warnings, error) {
	return []string{}, nil
}

func (c *CK8sConfigTemplate) ValidateDelete(_ context.Context, _ *CK8sConfigTemplate) (admission.Warnings, error) {
	return []string{}, nil
}
