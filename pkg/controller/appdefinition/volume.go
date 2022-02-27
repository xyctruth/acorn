package appdefinition

import (
	"strings"

	"github.com/ibuildthecloud/baaah/pkg/meta"
	"github.com/ibuildthecloud/baaah/pkg/router"
	"github.com/ibuildthecloud/baaah/pkg/typed"
	v1 "github.com/ibuildthecloud/herd/pkg/apis/herd-project.io/v1"
	"github.com/ibuildthecloud/herd/pkg/labels"
	name2 "github.com/rancher/wrangler/pkg/name"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func addPVCs(appInstance *v1.AppInstance, resp router.Response) {
	resp.Objects(toPVCs(appInstance)...)
}

func translateAccessModes(volumeRequest v1.VolumeRequest) (result []corev1.PersistentVolumeAccessMode) {
	for _, accessMode := range volumeRequest.AccessModes {
		newMode := strings.ToUpper(string(accessMode[0:1])) + string(accessMode[1:])
		result = append(result, corev1.PersistentVolumeAccessMode(newMode))
	}
	return
}

func toPVCs(appInstance *v1.AppInstance) (result []meta.Object) {
	for _, entry := range typed.Sorted(appInstance.Status.AppSpec.Volumes) {
		volume, volumeRequest := entry.Key, entry.Value
		if volumeRequest.Class == v1.VolumeRequestTypeEphemeral {
			continue
		}

		var (
			accessModes         = translateAccessModes(volumeRequest)
			volumeBinding, bind = isBind(appInstance, volume)
			pvc                 corev1.PersistentVolumeClaim
			class               *string
		)

		if bind {
			pvc = corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      bindName(volume),
					Namespace: appInstance.Status.Namespace,
					Labels: map[string]string{
						labels.HerdAppName:      appInstance.Name,
						labels.HerdAppNamespace: appInstance.Namespace,
					},
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: accessModes,
					VolumeName:  volumeBinding.Volume,
				},
			}
		} else {
			if volumeRequest.Class != "" {
				class = &volumeRequest.Class
			}
			pvc = corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      volume,
					Namespace: appInstance.Status.Namespace,
					Labels: map[string]string{
						labels.HerdAppName:      appInstance.Name,
						labels.HerdAppNamespace: appInstance.Namespace,
					},
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes:      accessModes,
					StorageClassName: class,
					VolumeName:       volumeBinding.Volume,
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: *resource.NewScaledQuantity(volumeRequest.Size, resource.Giga),
						},
					},
				},
			}
		}

		result = append(result, &pvc)
	}
	return
}

func isEphemeral(appInstance *v1.AppInstance, volume string) (v1.VolumeRequest, bool) {
	for name, volumeRequest := range appInstance.Status.AppSpec.Volumes {
		if name == volume && strings.EqualFold(volumeRequest.Class, v1.VolumeRequestTypeEphemeral) {
			return volumeRequest, true
		}
	}
	return v1.VolumeRequest{}, false
}

func isBind(appInstance *v1.AppInstance, volume string) (v1.VolumeBinding, bool) {
	for _, v := range appInstance.Spec.Volumes {
		if v.VolumeRequest == volume {
			return v, true
		}
	}
	return v1.VolumeBinding{}, false
}

func bindName(volume string) string {
	return name2.SafeConcatName(volume, "bind")
}

func toVolumeName(appInstance *v1.AppInstance, volume string) (string, bool) {
	if _, bind := isBind(appInstance, volume); bind {
		return bindName(volume), true
	}
	return volume, false
}

func toVolumes(appInstance *v1.AppInstance, container v1.Container) (result []corev1.Volume) {
	volumeNames := map[string]bool{}
	for _, volume := range container.Volumes {
		volumeNames[volume.Volume] = true
		for _, sidecar := range container.Sidecars {
			for _, volume := range sidecar.Volumes {
				volumeNames[volume.Volume] = true
			}
		}
	}

	for _, volume := range typed.SortedKeys(volumeNames) {
		name, bind := toVolumeName(appInstance, volume)
		if vr, ok := isEphemeral(appInstance, volume); ok && !bind {
			result = append(result, corev1.Volume{
				Name: volume,
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{
						SizeLimit: resource.NewScaledQuantity(int64(vr.Size), resource.Giga),
					},
				},
			})
		} else {
			result = append(result, corev1.Volume{
				Name: volume,
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: name,
					},
				},
			})
		}
	}

	return
}