/*
Copyright 2022.

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

package controllers

import (
	"context"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	prairiev1 "github.com/Tenacher/prairie-operator/api/v1"
)

// HomeAgentReconciler reconciles a HomeAgent object
type HomeAgentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=prairie.kismi,resources=homeagents,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=prairie.kismi,resources=homeagents/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=prairie.kismi,resources=homeagents/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the HomeAgent object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.13.0/pkg/reconcile
func (r *HomeAgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)
	log.Log.Info("Reconcile sequence has started.")
	wait_duration, _ := time.ParseDuration("800ms")

	home_agent := &prairiev1.HomeAgent{}
	err := r.Get(ctx, req.NamespacedName, home_agent)
	if err != nil {
		// Resource was most likely deleted before reconcile request,
		// thus we should clean up and return without requeueing.
		if errors.IsNotFound(err) {
			log.Log.Info("HomeAgent CRD not found.")

			r.DeleteDeployment(ctx, req)
			return ctrl.Result{}, nil
		}
		// Error reading object, requeue.
		return reconcile.Result{}, err
	}

	deployment := &appsv1.Deployment{}
	err = r.Get(ctx, req.NamespacedName, deployment)
	if err != nil {
		log.Log.Error(err, "Deployment is not ready.")
		if errors.IsNotFound(err) {
			err = r.Create(ctx, r.CreateDeployment(home_agent))

			if err != nil {
				return reconcile.Result{}, err
			}
			log.Log.Info("Deployment created, requeueing...")

			// We requeue to let the deployment get started
			return reconcile.Result{RequeueAfter: wait_duration}, nil
		} else {
			return reconcile.Result{}, err
		}
	}

	// Not every replica is ready, requeue
	if deployment.Status.ReadyReplicas < home_agent.Spec.Size {
		log.Log.Info("Not every replica is ready, requeueing...")
		return reconcile.Result{RequeueAfter: wait_duration}, nil
	}

	pods := &corev1.PodList{}
	err = r.List(ctx, pods, client.MatchingLabels{"parent": home_agent.Name})
	if err != nil {
		return ctrl.Result{}, err
	}

	podips := make([]string, home_agent.Spec.Size)
	for idx, pod := range pods.Items {
		ip := pod.Status.PodIP
		if ip == "" {
			log.Log.Info("Not every pod has ip, requeueing...")
			return ctrl.Result{RequeueAfter: wait_duration}, nil
		}
		podips[idx] = ip
	}

	home_agent.Status.NodeIps = podips

	err = r.Status().Update(ctx, home_agent)
	if err != nil {
		log.Log.Error(err, "HomeAgent status could not be updated.")
		return ctrl.Result{}, err
	}

	log.Log.Info("Reconcile sequence has successfully finished.")
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *HomeAgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&prairiev1.HomeAgent{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}

// Deletes deployment if it exists, simply returns otherwise
func (r *HomeAgentReconciler) DeleteDeployment(ctx context.Context, req ctrl.Request) {
	deployment := &appsv1.Deployment{}
	err := r.Get(ctx, req.NamespacedName, deployment)
	if err != nil {
		// Deployment no longer exists, we can safely return
		return
	}

	r.Delete(ctx, deployment)
}

func (r *HomeAgentReconciler) CreateDeployment(agent *prairiev1.HomeAgent) *appsv1.Deployment {
	labels := map[string]string{
		"parent": agent.Name,
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      agent.Name,
			Namespace: agent.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &agent.Spec.Size,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            "ha",
							Image:           "kismi/mo-daemon:latest",
							ImagePullPolicy: corev1.PullAlways,
							SecurityContext: &corev1.SecurityContext{
								Capabilities: &corev1.Capabilities{
									Add: []corev1.Capability{
										"NET_ADMIN",
									},
								},
							},
						},
					},
				},
			},
		},
	}
}
