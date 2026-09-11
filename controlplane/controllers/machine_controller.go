package controllers

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/canonical/cluster-api-k8s/pkg/ck8s"
)

// MachineReconciler reconciles a Machine object.
type MachineReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme

	K8sdDialTimeout time.Duration

	managementCluster ck8s.ManagementCluster
}

func (r *MachineReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, log *logr.Logger) error {
	_, err := ctrl.NewControllerManagedBy(mgr).
		For(&clusterv1.Machine{}).
		WithEventFilter(predicate.Funcs{
			CreateFunc: func(event.CreateEvent) bool { return true },
			// UpdateFunc must stay unconditional: this is what fires when DeletionTimestamp
			// transitions from zero to set, as well as annotation/condition changes.
			UpdateFunc:  func(event.UpdateEvent) bool { return true },
			DeleteFunc:  func(event.DeleteEvent) bool { return true },
			GenericFunc: func(event.GenericEvent) bool { return true },
		}).
		Build(r)

	if r.managementCluster == nil {
		r.managementCluster = &ck8s.Management{
			Client:          r.Client,
			K8sdDialTimeout: r.K8sdDialTimeout,
		}
	}

	return err
}

// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines;machines/status,verbs=get;list;watch;create;update;patch;delete

func (r *MachineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Log.WithValues("namespace", req.Namespace, "machine", req.Name)
	logger.Info("machine gets reconciled.")

	m := &clusterv1.Machine{}
	if err := r.Get(ctx, req.NamespacedName, m); err != nil {
		if apierrors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			logger.Info("machine not found.")
			return ctrl.Result{}, nil
		}

		// Error reading the object - requeue the request.
		logger.Info("machine could not be retrieved.")
		return ctrl.Result{}, err
	}

	logger.Info("machine gets deletion timestamp check")
	if m.DeletionTimestamp.IsZero() {
		logger.Info("machine does not have a deletion timestamp.")
		return ctrl.Result{RequeueAfter: 20 * time.Second}, nil
	}

	logger.Info("machine gets annotation check")
	// if machine registered PreTerminate hook, wait for capi asks to resolve PreTerminateDeleteHook
	if annotations.HasWithPrefix(clusterv1.PreTerminateDeleteHookAnnotationPrefix, m.Annotations) &&
		m.Annotations[PreTerminateHookCleanupAnnotation] == ck8sHookName {
		c := conditions.Get(m, clusterv1.MachineDeletingCondition)
		if c == nil || c.Status != metav1.ConditionTrue || c.Reason != clusterv1.MachineDeletingWaitingForPreTerminateHookReason {
			logger.Info("wait for machine drain and detach volume operation complete.")
			return ctrl.Result{}, nil
		}
		logger.Info("removing the annotation PreTerminateDeleteHookAnnotationPrefix")
		patchHelper, err := patch.NewHelper(m, r.Client)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to create patch helper for machine: %w", err)
		}

		mAnnotations := m.GetAnnotations()
		delete(mAnnotations, PreTerminateHookCleanupAnnotation)
		m.SetAnnotations(mAnnotations)
		if err := patchHelper.Patch(ctx, m); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to patch machine: %w", err)
		}
	}
	logger.Info("machine got annotation check", "annotation key", clusterv1.PreTerminateDeleteHookAnnotationPrefix)
	logger.Info("machine got annotation check", "annotation", m.Annotations[clusterv1.PreTerminateDeleteHookAnnotationPrefix])
	logger.Info("machine got annotation check", "annotation key", PreTerminateHookCleanupAnnotation)
	logger.Info("machine got annotation check", "annotation", m.Annotations[PreTerminateHookCleanupAnnotation])

	return ctrl.Result{}, nil
}
