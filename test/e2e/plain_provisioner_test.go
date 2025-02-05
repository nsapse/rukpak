package e2e

import (
	"context"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	"github.com/operator-framework/rukpak/internal/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// TODO: make this is a CLI flag?
	defaultSystemNamespace = "rukpak-system"
	plainProvisionerID     = "core.rukpak.io/plain"
)

func Logf(f string, v ...interface{}) {
	fmt.Fprintf(GinkgoWriter, f, v...)
}

var _ = Describe("plain provisioner bundle", func() {
	When("a valid Bundle referencing a remote container image is created", func() {
		var (
			bundle *rukpakv1alpha1.Bundle
			ctx    context.Context
		)
		BeforeEach(func() {
			ctx = context.Background()

			By("creating the testing Bundle resource")
			bundle = &rukpakv1alpha1.Bundle{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "olm-crds-valid",
				},
				Spec: rukpakv1alpha1.BundleSpec{
					ProvisionerClassName: plainProvisionerID,
					Image:                "quay.io/tflannag/olm-plain-bundle:olm-crds-v0.20.0",
				},
			}
			err := c.Create(ctx, bundle)
			Expect(err).To(BeNil())
		})
		AfterEach(func() {
			By("deleting the testing Bundle resource")
			err := c.Delete(ctx, bundle)
			Expect(err).To(BeNil())
		})

		It("should eventually report a successful state", func() {
			By("eventually reporting an Unpacked phase", func() {
				Eventually(func() bool {
					if err := c.Get(ctx, client.ObjectKeyFromObject(bundle), bundle); err != nil {
						return false
					}
					return bundle.Status.Phase == rukpakv1alpha1.PhaseUnpacked
				}).Should(BeTrue())
			})

			By("eventually writing a non-empty image digest to the status", func() {
				Eventually(func() bool {
					if err := c.Get(ctx, client.ObjectKeyFromObject(bundle), bundle); err != nil {
						return false
					}
					// Note(tflannag): This may make the test fairly brittle over time. Let's re-evaluate
					// whether we want to test this kind of comparison if there's the potential for moving
					// tags for Bundle container images.
					expectedDigest := "quay.io/tflannag/olm-plain-bundle@sha256:2a72e4a7bc6c7598d1ecdb2f082c865a91dda35ab8e4a8e8bc128cf49a2a619b"
					return bundle.Status.Digest == expectedDigest
				}).Should(BeTrue())
			})

			By("eventually writing a non-empty list of unpacked objects to the status", func() {
				Eventually(func() bool {
					if err := c.Get(ctx, client.ObjectKeyFromObject(bundle), bundle); err != nil {
						return false
					}
					if bundle.Status.Info == nil {
						return false
					}
					return len(bundle.Status.Info.Objects) == 8
				}).Should(BeTrue())
			})
		})

		It("should re-create the bundle configmaps", func() {
			var (
				cm *corev1.ConfigMap
			)

			By("getting the metadata configmap for the bundle")
			// TODO(tflannag): Create a constant for the "core.rukpak.io/configmap-type" label
			// and update the internal/controller packages.
			configMapTypeRequirement, err := labels.NewRequirement("core.rukpak.io/configmap-type", selection.Equals, []string{"metadata"})
			Expect(err).To(BeNil())

			selector := util.NewBundleLabelSelector(bundle)
			selector = selector.Add(*configMapTypeRequirement)

			Eventually(func() bool {
				cms := &corev1.ConfigMapList{}
				if err := c.List(ctx, cms, &client.ListOptions{
					Namespace:     defaultSystemNamespace,
					LabelSelector: selector,
				}); err != nil {
					return false
				}
				if len(cms.Items) != 1 {
					return false
				}
				cm = &cms.Items[0]

				return true
			}).Should(BeTrue())

			By("deleting the metadata configmap and checking if it gets recreated")
			originalUID := cm.GetUID()

			Eventually(func() error {
				return c.Delete(ctx, cm)
			}).Should(Succeed())

			Eventually(func() (types.UID, error) {
				err := c.Get(ctx, client.ObjectKeyFromObject(cm), cm)
				return cm.GetUID(), err
			}).ShouldNot(Equal(originalUID))
		})

		It("should re-create underlying system resources", func() {
			var (
				pod *corev1.Pod
			)

			By("getting the underlying bundle unpacking pod")
			selector := util.NewBundleLabelSelector(bundle)
			Eventually(func() bool {
				pods := &corev1.PodList{}
				if err := c.List(ctx, pods, &client.ListOptions{
					Namespace:     defaultSystemNamespace,
					LabelSelector: selector,
				}); err != nil {
					return false
				}
				if len(pods.Items) != 1 {
					return false
				}
				pod = &pods.Items[0]
				return true
			}).Should(BeTrue())

			By("storing the pod's original UID")
			originalUID := pod.GetUID()

			By("deleting the underlying pod and waiting for it to be re-created")
			err := c.Delete(context.Background(), pod)
			Expect(err).To(BeNil())

			By("verifying the pod's UID has changed")
			Eventually(func() (types.UID, error) {
				err := c.Get(ctx, client.ObjectKeyFromObject(pod), pod)
				return pod.GetUID(), err
			}).ShouldNot(Equal(originalUID))
		})
	})

	When("an invalid Bundle referencing a remote container image is created", func() {
		var (
			bundle *rukpakv1alpha1.Bundle
			ctx    context.Context
		)
		BeforeEach(func() {
			ctx = context.Background()

			By("creating the testing Bundle resource")
			bundle = &rukpakv1alpha1.Bundle{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "olm-crds-invalid",
				},
				Spec: rukpakv1alpha1.BundleSpec{
					ProvisionerClassName: "core.rukpak.io/plain",
					Image:                "quay.io/tflannag/olm-plain-bundle:non-existent-tag",
				},
			}
			err := c.Create(ctx, bundle)
			Expect(err).To(BeNil())
		})
		AfterEach(func() {
			By("deleting the testing Bundle resource")
			err := c.Delete(ctx, bundle)
			Expect(err).To(BeNil())
		})

		It("checks the bundle's phase is stuck in pending", func() {
			By("waiting until the pod is reporting ImagePullBackOff state")
			Eventually(func() bool {
				pod := &corev1.Pod{}
				if err := c.Get(ctx, types.NamespacedName{
					Name:      util.PodName("plain", bundle.GetName()),
					Namespace: defaultSystemNamespace,
				}, pod); err != nil {
					return false
				}
				if pod.Status.Phase != corev1.PodPending {
					return false
				}
				for _, status := range pod.Status.ContainerStatuses {
					if status.State.Waiting != nil && status.State.Waiting.Reason == "ImagePullBackOff" {
						return true
					}
				}
				return false
			}).Should(BeTrue())

			By("waiting for the bundle to report back that state")
			Eventually(func() bool {
				err := c.Get(ctx, client.ObjectKeyFromObject(bundle), bundle)
				if err != nil {
					return false
				}
				if bundle.Status.Phase != rukpakv1alpha1.PhasePending {
					return false
				}
				unpackPending := meta.FindStatusCondition(bundle.Status.Conditions, rukpakv1alpha1.PhaseUnpacked)
				if unpackPending == nil {
					return false
				}
				if unpackPending.Message != fmt.Sprintf(`Back-off pulling image "%s"`, bundle.Spec.Image) {
					return false
				}
				return true
			}).Should(BeTrue())
		})
	})
})

var _ = Describe("plain provisioner bundleinstance", func() {
	When("a BundleInstance targets a valid Bundle", func() {
		var (
			bundle *rukpakv1alpha1.Bundle
			bi     *rukpakv1alpha1.BundleInstance
			ctx    context.Context
		)
		BeforeEach(func() {
			ctx = context.Background()

			By("creating the testing Bundle resource")
			bundle = &rukpakv1alpha1.Bundle{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "olm-crds",
				},
				Spec: rukpakv1alpha1.BundleSpec{
					ProvisionerClassName: plainProvisionerID,
					Image:                "quay.io/tflannag/olm-plain-bundle:olm-crds-v0.20.0",
				},
			}
			err := c.Create(ctx, bundle)
			Expect(err).To(BeNil())

			bi = &rukpakv1alpha1.BundleInstance{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "olm-crds",
				},
				Spec: rukpakv1alpha1.BundleInstanceSpec{
					ProvisionerClassName: plainProvisionerID,
					BundleName:           bundle.GetName(),
				},
			}
			err = c.Create(ctx, bi)
			Expect(err).To(BeNil())
		})
		AfterEach(func() {
			By("deleting the testing Bundle resource")
			Eventually(func() error {
				return client.IgnoreNotFound(c.Delete(ctx, bundle))
			}).Should(Succeed())

			By("deleting the testing BundleInstance resource")
			Eventually(func() error {
				return c.Delete(ctx, bi)
			}).Should(Succeed())
		})

		It("should rollout the bundle contents successfully", func() {
			By("eventually writing a successful installation state back to the bundleinstance status")
			Eventually(func() bool {
				if err := c.Get(ctx, client.ObjectKeyFromObject(bi), bi); err != nil {
					return false
				}
				if bi.Status.InstalledBundleName != bundle.GetName() {
					return false
				}
				existing := meta.FindStatusCondition(bi.Status.Conditions, "Installed")
				if existing == nil {
					return false
				}
				expected := metav1.Condition{
					Type:    "Installed",
					Status:  metav1.ConditionStatus(corev1.ConditionTrue),
					Reason:  "InstallationSucceeded",
					Message: "",
				}
				return conditionsSemanticallyEqual(expected, *existing)
			}).Should(BeTrue())

			By("eventually reseting a bundle lookup failure when the targeted bundle has been deleted")
			Eventually(func() error {
				return c.Delete(ctx, bundle)
			}).Should(Succeed())

			By("eventually having a status indicating that the bundle lookup failed but installation succeeded")
			Eventually(func() bool {
				if err := c.Get(ctx, client.ObjectKeyFromObject(bi), bi); err != nil {
					return false
				}

				// Should still have the original InstalledBundleName in the status
				if bi.Status.InstalledBundleName == "" {
					return false
				}

				// Find the installed condition
				existingInstalledStatus := meta.FindStatusCondition(bi.Status.Conditions, "Installed")
				if existingInstalledStatus == nil {
					return false
				}
				expectedInstalledStatus := metav1.Condition{
					Type:    "Installed",
					Status:  metav1.ConditionStatus(corev1.ConditionTrue),
					Reason:  "InstallationSucceeded",
					Message: "",
				}

				// Find the HasValidBundle status
				existingBundleStatus := meta.FindStatusCondition(bi.Status.Conditions, "HasValidBundle")
				if existingBundleStatus == nil {
					return false
				}
				expectedBundleStatus := metav1.Condition{
					Type:    "HasValidBundle",
					Status:  metav1.ConditionStatus(corev1.ConditionFalse),
					Reason:  "BundleLookupFailed",
					Message: fmt.Sprintf(`Bundle.core.rukpak.io "%s" not found`, bundle.GetName()),
				}

				return conditionsSemanticallyEqual(expectedInstalledStatus, *existingInstalledStatus) &&
					conditionsSemanticallyEqual(expectedBundleStatus, *existingBundleStatus)
			}).Should(BeTrue())
		})
	})

	When("a BundleInstance targets an invalid Bundle", func() {
		var (
			bundle *rukpakv1alpha1.Bundle
			bi     *rukpakv1alpha1.BundleInstance
			ctx    context.Context
		)
		BeforeEach(func() {
			ctx = context.Background()

			By("creating the testing Bundle resource")
			bundle = &rukpakv1alpha1.Bundle{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "olm-apis",
				},
				Spec: rukpakv1alpha1.BundleSpec{
					ProvisionerClassName: plainProvisionerID,
					Image:                "quay.io/tflannag/olm-plain-bundle:olm-api-v0.20.0",
				},
			}
			err := c.Create(ctx, bundle)
			Expect(err).To(BeNil())

			bi = &rukpakv1alpha1.BundleInstance{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "olm-apis",
				},
				Spec: rukpakv1alpha1.BundleInstanceSpec{
					ProvisionerClassName: plainProvisionerID,
					BundleName:           bundle.GetName(),
				},
			}
			err = c.Create(ctx, bi)
			Expect(err).To(BeNil())
		})
		AfterEach(func() {
			By("deleting the testing Bundle resource")
			Eventually(func() error {
				return client.IgnoreNotFound(c.Delete(ctx, bundle))
			}).Should(Succeed())

			By("deleting the testing BundleInstance resource")
			Eventually(func() error {
				return client.IgnoreNotFound(c.Delete(ctx, bi))
			}).Should(Succeed())
		})

		It("should project a failed installation state", func() {
			Eventually(func() bool {
				if err := c.Get(ctx, client.ObjectKeyFromObject(bi), bi); err != nil {
					return false
				}
				if bi.Status.InstalledBundleName != "" {
					return false
				}
				existing := meta.FindStatusCondition(bi.Status.Conditions, "Installed")
				if existing == nil {
					return false
				}
				// TODO(tflannag): Add a custom error type for API-based Bundle installations that
				// are missing the requisite CRDs to be able to deploy the unpacked Bundle successfully.
				expectedMessage := `
				error while running post render on files: [unable to recognize "": no
				matches for kind "CatalogSource" in version "operators.coreos.com/v1alpha1",
				unable to recognize "": no matches for kind "ClusterServiceVersion" in version
				"operators.coreos.com/v1alpha1", unable to recognize "": no matches for kind
				"OLMConfig" in version "operators.coreos.com/v1", unable to recognize "": no
				matches for kind "OperatorGroup" in version "operators.coreos.com/v1"]
				`
				expected := metav1.Condition{
					Type:    "Installed",
					Status:  metav1.ConditionStatus(corev1.ConditionFalse),
					Reason:  "InstallFailed",
					Message: strings.Join(strings.Fields(expectedMessage), " "),
				}
				return conditionsSemanticallyEqual(expected, *existing)
			}).Should(BeTrue())
		})
	})

	When("a BundleInstance targets a valid bundle but the bundle contains resources that already exist", func() {
		var (
			bundle      *rukpakv1alpha1.Bundle
			biOriginal  *rukpakv1alpha1.BundleInstance
			biDuplicate *rukpakv1alpha1.BundleInstance
			ctx         context.Context
		)
		BeforeEach(func() {
			ctx = context.Background()

			By("creating the testing Bundle resource")
			bundle = &rukpakv1alpha1.Bundle{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "olm-crds-valid",
				},
				Spec: rukpakv1alpha1.BundleSpec{
					ProvisionerClassName: plainProvisionerID,
					Image:                "quay.io/tflannag/olm-plain-bundle:olm-crds-v0.20.0",
				},
			}
			err := c.Create(ctx, bundle)
			Expect(err).To(BeNil())

			biOriginal = &rukpakv1alpha1.BundleInstance{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "olm-apis-original",
				},
				Spec: rukpakv1alpha1.BundleInstanceSpec{
					ProvisionerClassName: plainProvisionerID,
					BundleName:           bundle.GetName(),
				},
			}

			err = c.Create(ctx, biOriginal)
			Expect(err).To(BeNil(), "failed to create original bundle instance")
			Eventually(func() error { // Guarantee that the original bundle instance is create prior to the duplicate
				return c.Get(ctx, client.ObjectKeyFromObject(biOriginal), &rukpakv1alpha1.BundleInstance{})
			}).Should(Succeed(), "failed to validate original bundle instance was created in time")

			biDuplicate = &rukpakv1alpha1.BundleInstance{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "olm-apis-duplicate",
				},
				Spec: rukpakv1alpha1.BundleInstanceSpec{
					ProvisionerClassName: plainProvisionerID,
					BundleName:           bundle.GetName(),
				},
			}

			err = c.Create(ctx, biDuplicate)
			Expect(err).To(BeNil(), "failed to create duplicate bundle instance")
		})

		AfterEach(func() {
			By("deleting the testing Bundle resource")
			Eventually(func() error {
				return client.IgnoreNotFound(c.Delete(ctx, bundle))
			}).Should(Succeed())

			By("deleting the testing original BundleInstance resource")
			Eventually(func() error {
				return client.IgnoreNotFound(c.Delete(ctx, biOriginal))
			}).Should(Succeed())

			By("deleting the testing duplicate BundleInstance resource")
			Eventually(func() error {
				return client.IgnoreNotFound(c.Delete(ctx, biDuplicate))
			}).Should(Succeed())
		})

		It("should succeed installation for the original but fail for the duplicate BundleInstance", func() {
			By("projecting a successful installation status for the original BundleInstance")
			Eventually(func() bool {
				if err := c.Get(ctx, client.ObjectKeyFromObject(biOriginal), biOriginal); err != nil {
					return false
				}
				if biOriginal.Status.InstalledBundleName != "" {
					return false
				}
				existing := meta.FindStatusCondition(biOriginal.Status.Conditions, "Installed")
				return existing != nil
			})

			By("projecting a failed installation status for the duplicate BundleInstance")
			Eventually(func() bool {
				if err := c.Get(ctx, client.ObjectKeyFromObject(biDuplicate), biDuplicate); err != nil {
					return false
				}
				if biDuplicate.Status.InstalledBundleName != "" {
					return false
				}
				existing := meta.FindStatusCondition(biDuplicate.Status.Conditions, "Installed")
				if existing == nil {
					return false
				}
				// Create what the message should contain
				looselyExpectedMessage := "rendered manifests contain a resource that already exists"
				expected := metav1.Condition{
					Type:    "Installed",
					Status:  metav1.ConditionStatus(corev1.ConditionFalse),
					Reason:  "InstallFailed",
					Message: looselyExpectedMessage,
				}

				return conditionsLooselyEqual(expected, *existing)
			}).Should(BeTrue())
		})
	})
})

func conditionsSemanticallyEqual(a, b metav1.Condition) bool {
	return a.Type == b.Type && a.Status == b.Status && a.Reason == b.Reason && a.Message == b.Message
}

func conditionsLooselyEqual(a, b metav1.Condition) bool {
	return a.Type == b.Type && a.Status == b.Status && a.Reason == b.Reason && strings.Contains(b.Message, a.Message)
}
