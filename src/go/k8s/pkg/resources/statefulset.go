// Copyright 2021 Vectorized, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package resources contains reconciliation logic for redpanda.vectorized.io CRD
package resources

import (
	"context"
	"fmt"
	"strings"

	redpandav1alpha1 "github.com/vectorizedio/redpanda/src/go/k8s/apis/redpanda/v1alpha1"
	"github.com/vectorizedio/redpanda/src/go/k8s/pkg/labels"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

var _ Resource = &StatefulSetResource{}

// StatefulSetResource is part of the reconciliation of redpanda.vectorized.io CRD
// focusing on the management of redpanda cluster
type StatefulSetResource struct {
	k8sclient.Client
	scheme		*runtime.Scheme
	pandaCluster	*redpandav1alpha1.Cluster

	LastObservedState	*appsv1.StatefulSet
}

// NewStatefulSet creates StatefulSetResource
func NewStatefulSet(
	client k8sclient.Client,
	pandaCluster *redpandav1alpha1.Cluster,
	scheme *runtime.Scheme,
) *StatefulSetResource {
	return &StatefulSetResource{
		client, scheme, pandaCluster, nil,
	}
}

// Ensure will manage kubernetes v1.StatefulSet for redpanda.vectorized.io custom resource
func (r *StatefulSetResource) Ensure(ctx context.Context) error {
	var sts appsv1.StatefulSet

	err := r.Get(ctx, r.Key(), &sts)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	if errors.IsNotFound(err) {
		obj, err := r.Obj()
		if err != nil {
			return err
		}

		err = r.Create(ctx, obj)
		r.LastObservedState = obj.(*appsv1.StatefulSet)

		return err
	}

	r.LastObservedState = &sts

	// Ensure StatefulSet #replicas equals cluster requirement.
	if sts.Spec.Replicas != r.pandaCluster.Spec.Replicas {
		sts.Spec.Replicas = r.pandaCluster.Spec.Replicas
		if err := r.Update(ctx, &sts); err != nil {
			return fmt.Errorf("failed to update StatefulSet: %w", err)
		}
	}

	return nil
}

// Obj returns resource managed client.Object
// nolint:funlen // The complexity of Obj function will be address in the next version TODO
func (r *StatefulSetResource) Obj() (k8sclient.Object, error) {
	var configMapDefaultMode int32 = 0754

	memory, exist := r.pandaCluster.Spec.Resources.Limits["memory"]
	if !exist {
		memory = resource.MustParse("2Gi")
	}

	ss := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:	r.Key().Namespace,
			Name:		r.Key().Name,
			Labels:		labels.ForCluster(r.pandaCluster),
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:		pointer.Int32Ptr(1),
			PodManagementPolicy:	appsv1.ParallelPodManagement,
			Selector:		metav1.SetAsLabelSelector(r.pandaCluster.Labels),
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.RollingUpdateStatefulSetStrategyType,
			},
			ServiceName:	r.pandaCluster.Name,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:		r.pandaCluster.Name,
					Namespace:	r.pandaCluster.Namespace,
					Labels:		r.pandaCluster.Labels,
				},
				Spec: corev1.PodSpec{
					SecurityContext: &corev1.PodSecurityContext{
						FSGroup: pointer.Int64Ptr(fsGroup),
					},
					Volumes: []corev1.Volume{
						{
							Name:	"datadir",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: "datadir",
								},
							},
						},
						{
							Name:	"configmap-dir",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: ConfigMapKey(r.pandaCluster).Name,
									},
									DefaultMode:	&configMapDefaultMode,
								},
							},
						},
						{
							Name:	"config-dir",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
					InitContainers: []corev1.Container{
						{
							Name:		"redpanda-configurator",
							Image:		r.pandaCluster.Spec.Image + ":" + r.pandaCluster.Spec.Version,
							Command:	[]string{"/bin/sh", "-c"},
							Args:		[]string{configuratorPath},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:		"config-dir",
									MountPath:	configDir,
								},
								{
									Name:		"configmap-dir",
									MountPath:	configuratorDir,
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:	"redpanda",
							Image:	r.pandaCluster.Spec.Image + ":" + r.pandaCluster.Spec.Version,
							Args: []string{
								"--check=false",
								"--smp 1",
								"--memory " + strings.ReplaceAll(memory.String(), "Gi", "G"),
								"start",
								"--",
								"--default-log-level=debug",
								"--reserve-memory 0M",
							},
							Env: []corev1.EnvVar{
								{
									Name:	"REDPANDA_ENVIRONMENT",
									Value:	"kubernetes",
								},
							},
							Ports: []corev1.ContainerPort{
								{
									Name:		"admin",
									ContainerPort:	int32(r.pandaCluster.Spec.Configuration.AdminAPI.Port),
								},
								{
									Name:		"kafka",
									ContainerPort:	int32(r.pandaCluster.Spec.Configuration.KafkaAPI.Port),
								},
								{
									Name:		"rpc",
									ContainerPort:	int32(r.pandaCluster.Spec.Configuration.RPCServer.Port),
								},
							},
							Resources: corev1.ResourceRequirements{
								Limits:		r.pandaCluster.Spec.Resources.Limits,
								Requests:	r.pandaCluster.Spec.Resources.Requests,
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:		"datadir",
									MountPath:	dataDirectory,
								},
								{
									Name:		"config-dir",
									MountPath:	configDir,
								},
							},
						},
					},
					Affinity: &corev1.Affinity{
						PodAntiAffinity: &corev1.PodAntiAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
								{
									LabelSelector:	metav1.SetAsLabelSelector(r.pandaCluster.Labels),
									Namespaces:	[]string{r.pandaCluster.Namespace},
									TopologyKey:	corev1.LabelHostname},
							},
							PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
								{
									Weight:	100,
									PodAffinityTerm: corev1.PodAffinityTerm{
										LabelSelector:	metav1.SetAsLabelSelector(r.pandaCluster.Labels),
										Namespaces:	[]string{r.pandaCluster.Namespace},
										TopologyKey:	corev1.LabelHostname,
									},
								},
							},
						},
					},
					TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
						{
							MaxSkew:		1,
							TopologyKey:		corev1.LabelZoneFailureDomainStable,
							WhenUnsatisfiable:	corev1.ScheduleAnyway,
							LabelSelector:		metav1.SetAsLabelSelector(r.pandaCluster.Labels),
						},
					},
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace:	r.pandaCluster.Namespace,
						Name:		"datadir",
						Labels:		r.pandaCluster.Labels,
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes:	[]corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								"storage": resource.MustParse("100Gi"),
							},
						},
					},
				},
			},
		},
	}

	err := controllerutil.SetControllerReference(r.pandaCluster, ss, r.scheme)
	if err != nil {
		return nil, err
	}

	return ss, nil
}

// Key returns namespace/name object that is used to identify object.
// For reference please visit types.NamespacedName docs in k8s.io/apimachinery
func (r *StatefulSetResource) Key() types.NamespacedName {
	return types.NamespacedName{Name: r.pandaCluster.Name, Namespace: r.pandaCluster.Namespace}
}

// Kind returns v1.StatefulSet kind
func (r *StatefulSetResource) Kind() string {
	var statefulSet appsv1.StatefulSet
	return statefulSet.Kind
}
