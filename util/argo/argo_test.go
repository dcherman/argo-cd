package argo

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	testcore "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	argoappv1 "github.com/argoproj/argo-cd/pkg/apis/application/v1alpha1"
	appclientset "github.com/argoproj/argo-cd/pkg/client/clientset/versioned/fake"
	"github.com/argoproj/argo-cd/pkg/client/informers/externalversions/application/v1alpha1"
	applisters "github.com/argoproj/argo-cd/pkg/client/listers/application/v1alpha1"
	"github.com/argoproj/argo-cd/reposerver/apiclient"
)

func TestRefreshApp(t *testing.T) {
	var testApp argoappv1.Application
	testApp.Name = "test-app"
	testApp.Namespace = "default"
	appClientset := appclientset.NewSimpleClientset(&testApp)
	appIf := appClientset.ArgoprojV1alpha1().Applications("default")
	_, err := RefreshApp(appIf, "test-app", argoappv1.RefreshTypeNormal)
	assert.Nil(t, err)
	// For some reason, the fake Application inferface doesn't reflect the patch status after Patch(),
	// so can't verify it was set in unit tests.
	//_, ok := newApp.Annotations[common.AnnotationKeyRefresh]
	//assert.True(t, ok)
}

func TestGetAppProjectWithNoProjDefined(t *testing.T) {
	projName := "default"
	namespace := "default"

	testProj := &argoappv1.AppProject{
		ObjectMeta: metav1.ObjectMeta{Name: projName, Namespace: namespace},
	}

	var testApp argoappv1.Application
	testApp.Name = "test-app"
	testApp.Namespace = namespace
	appClientset := appclientset.NewSimpleClientset(testProj)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	informer := v1alpha1.NewAppProjectInformer(appClientset, namespace, 0, cache.Indexers{})
	go informer.Run(ctx.Done())
	cache.WaitForCacheSync(ctx.Done(), informer.HasSynced)
	proj, err := GetAppProject(&testApp.Spec, applisters.NewAppProjectLister(informer.GetIndexer()), namespace)
	assert.Nil(t, err)
	assert.Equal(t, proj.Name, projName)
}

func TestWaitForRefresh(t *testing.T) {
	appClientset := appclientset.NewSimpleClientset()

	// Verify timeout
	appIf := appClientset.ArgoprojV1alpha1().Applications("default")
	oneHundredMs := 100 * time.Millisecond
	app, err := WaitForRefresh(context.Background(), appIf, "test-app", &oneHundredMs)
	assert.NotNil(t, err)
	assert.Nil(t, app)
	assert.Contains(t, strings.ToLower(err.Error()), "deadline exceeded")

	// Verify success
	var testApp argoappv1.Application
	testApp.Name = "test-app"
	testApp.Namespace = "default"
	appClientset = appclientset.NewSimpleClientset()

	appIf = appClientset.ArgoprojV1alpha1().Applications("default")
	watcher := watch.NewFake()
	appClientset.PrependWatchReactor("applications", testcore.DefaultWatchReactor(watcher, nil))
	// simulate add/update/delete watch events
	go watcher.Add(&testApp)
	app, err = WaitForRefresh(context.Background(), appIf, "test-app", &oneHundredMs)
	assert.Nil(t, err)
	assert.NotNil(t, app)
}

func TestContainsSyncResource(t *testing.T) {
	var (
		blankUnstructured unstructured.Unstructured
		blankResource     argoappv1.SyncOperationResource
		helloResource     = argoappv1.SyncOperationResource{Name: "hello"}
	)
	tables := []struct {
		u        *unstructured.Unstructured
		rr       []argoappv1.SyncOperationResource
		expected bool
	}{
		{&blankUnstructured, []argoappv1.SyncOperationResource{}, false},
		{&blankUnstructured, []argoappv1.SyncOperationResource{blankResource}, true},
		{&blankUnstructured, []argoappv1.SyncOperationResource{helloResource}, false},
	}

	for _, table := range tables {
		if out := ContainsSyncResource(table.u.GetName(), table.u.GroupVersionKind(), table.rr); out != table.expected {
			t.Errorf("Expected %t for slice %+v conains resource %+v; instead got %t", table.expected, table.rr, table.u, out)
		}
	}
}

// TestNilOutZerValueAppSources verifies we will nil out app source specs when they are their zero-value
func TestNilOutZerValueAppSources(t *testing.T) {
	var spec *argoappv1.ApplicationSpec
	{
		spec = NormalizeApplicationSpec(&argoappv1.ApplicationSpec{Source: argoappv1.ApplicationSource{Kustomize: &argoappv1.ApplicationSourceKustomize{NamePrefix: "foo"}}})
		assert.NotNil(t, spec.Source.Kustomize)
		spec = NormalizeApplicationSpec(&argoappv1.ApplicationSpec{Source: argoappv1.ApplicationSource{Kustomize: &argoappv1.ApplicationSourceKustomize{NamePrefix: ""}}})
		assert.Nil(t, spec.Source.Kustomize)
	}
	{
		spec = NormalizeApplicationSpec(&argoappv1.ApplicationSpec{Source: argoappv1.ApplicationSource{Helm: &argoappv1.ApplicationSourceHelm{ValueFiles: []string{"values.yaml"}}}})
		assert.NotNil(t, spec.Source.Helm)
		spec = NormalizeApplicationSpec(&argoappv1.ApplicationSpec{Source: argoappv1.ApplicationSource{Helm: &argoappv1.ApplicationSourceHelm{ValueFiles: []string{}}}})
		assert.Nil(t, spec.Source.Helm)
	}
	{
		spec = NormalizeApplicationSpec(&argoappv1.ApplicationSpec{Source: argoappv1.ApplicationSource{Ksonnet: &argoappv1.ApplicationSourceKsonnet{Environment: "foo"}}})
		assert.NotNil(t, spec.Source.Ksonnet)
		spec = NormalizeApplicationSpec(&argoappv1.ApplicationSpec{Source: argoappv1.ApplicationSource{Ksonnet: &argoappv1.ApplicationSourceKsonnet{Environment: ""}}})
		assert.Nil(t, spec.Source.Ksonnet)
	}
	{
		spec = NormalizeApplicationSpec(&argoappv1.ApplicationSpec{Source: argoappv1.ApplicationSource{Directory: &argoappv1.ApplicationSourceDirectory{Recurse: true}}})
		assert.NotNil(t, spec.Source.Directory)
		spec = NormalizeApplicationSpec(&argoappv1.ApplicationSpec{Source: argoappv1.ApplicationSource{Directory: &argoappv1.ApplicationSourceDirectory{Recurse: false}}})
		assert.Nil(t, spec.Source.Directory)
	}
}

func TestValidatePermissionsEmptyDestination(t *testing.T) {
	conditions, err := ValidatePermissions(context.Background(), &argoappv1.ApplicationSpec{
		Source: argoappv1.ApplicationSource{RepoURL: "https://github.com/argoproj/argo-cd", Path: "."},
	}, &argoappv1.AppProject{
		Spec: argoappv1.AppProjectSpec{
			SourceRepos:  []string{"*"},
			Destinations: []argoappv1.ApplicationDestination{{Server: "*", Namespace: "*"}},
		},
	}, nil)
	assert.NoError(t, err)
	assert.ElementsMatch(t, conditions, []argoappv1.ApplicationCondition{{Type: argoappv1.ApplicationConditionInvalidSpecError, Message: "Destination server and/or namespace missing from app spec"}})
}

func TestValidateChartWithoutRevision(t *testing.T) {
	conditions, err := ValidatePermissions(context.Background(), &argoappv1.ApplicationSpec{
		Source: argoappv1.ApplicationSource{RepoURL: "https://kubernetes-charts-incubator.storage.googleapis.com/", Chart: "myChart", TargetRevision: ""},
		Destination: argoappv1.ApplicationDestination{
			Server: "https://kubernetes.default.svc", Namespace: "default",
		},
	}, &argoappv1.AppProject{
		Spec: argoappv1.AppProjectSpec{
			SourceRepos:  []string{"*"},
			Destinations: []argoappv1.ApplicationDestination{{Server: "*", Namespace: "*"}},
		},
	}, nil)
	assert.NoError(t, err)
	assert.ElementsMatch(t, conditions, []argoappv1.ApplicationCondition{{
		Type: argoappv1.ApplicationConditionInvalidSpecError, Message: "spec.source.targetRevision is required if the manifest source is a helm chart"}})
}

func Test_enrichSpec(t *testing.T) {
	t.Run("Empty", func(t *testing.T) {
		spec := &argoappv1.ApplicationSpec{}
		enrichSpec(spec, &apiclient.RepoAppDetailsResponse{})
		assert.Empty(t, spec.Destination.Server)
		assert.Empty(t, spec.Destination.Namespace)
	})
	t.Run("Ksonnet", func(t *testing.T) {
		spec := &argoappv1.ApplicationSpec{
			Source: argoappv1.ApplicationSource{
				Ksonnet: &argoappv1.ApplicationSourceKsonnet{
					Environment: "qa",
				},
			},
		}
		response := &apiclient.RepoAppDetailsResponse{
			Ksonnet: &apiclient.KsonnetAppSpec{
				Environments: map[string]*apiclient.KsonnetEnvironment{
					"prod": {
						Destination: &apiclient.KsonnetEnvironmentDestination{
							Server:    "my-server",
							Namespace: "my-namespace",
						},
					},
				},
			},
		}
		enrichSpec(spec, response)
		assert.Empty(t, spec.Destination.Server)
		assert.Empty(t, spec.Destination.Namespace)

		spec.Source.Ksonnet.Environment = "prod"
		enrichSpec(spec, response)
		assert.Equal(t, "my-server", spec.Destination.Server)
		assert.Equal(t, "my-namespace", spec.Destination.Namespace)
	})
}

func createHelmApplicationSpec() *argoappv1.ApplicationSpec {
	return &argoappv1.ApplicationSpec{
		Source: argoappv1.ApplicationSource{
			Helm: &argoappv1.ApplicationSourceHelm{},
		},
	}
}

func TestResolveHelmValues(t *testing.T) {
	trueValue := true

	t.Run("Non Helm Application", func(t *testing.T) {
		const testNamespace = "argocd"

		spec := &argoappv1.ApplicationSpec{
			Source: argoappv1.ApplicationSource{
				Path:    "foobar",
				RepoURL: "https://test.com",
			},
		}

		kubeclientset := fake.NewSimpleClientset()

		value, err := ResolveHelmValues(kubeclientset, testNamespace, spec)
		assert.Nil(t, err)
		assert.Equal(t, "{}\n", value)
	})

	t.Run("No additional secrets/configmaps", func(t *testing.T) {
		const testNamespace = "argocd"

		spec := createHelmApplicationSpec()

		kubeclientset := fake.NewSimpleClientset()

		value, err := ResolveHelmValues(kubeclientset, testNamespace, spec)
		assert.Nil(t, err)
		assert.Equal(t, "{}\n", value)
	})

	t.Run("Single ConfigMap", func(t *testing.T) {
		const testNamespace = "argocd"
		spec := createHelmApplicationSpec()
		spec.Source.Helm.ValuesFrom = append(spec.Source.Helm.ValuesFrom, argoappv1.ValuesFromSource{
			ConfigMapKeyRef: &v1.ConfigMapKeySelector{
				LocalObjectReference: v1.LocalObjectReference{
					Name: "additional-helm-values",
				},
			},
		})

		kubeclientset := fake.NewSimpleClientset(&v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testNamespace,
				Name:      "additional-helm-values",
			},
			Data: map[string]string{
				"values.yaml": "baz: 'quux'\n",
			},
		})

		value, err := ResolveHelmValues(kubeclientset, testNamespace, spec)
		assert.Nil(t, err)
		assert.Equal(t, "baz: quux\n", value)
	})

	t.Run("Multiple ConfigMap", func(t *testing.T) {
		const testNamespace = "argocd"
		spec := createHelmApplicationSpec()

		spec.Source.Helm.ValuesFrom = append(spec.Source.Helm.ValuesFrom, argoappv1.ValuesFromSource{
			ConfigMapKeyRef: &v1.ConfigMapKeySelector{
				LocalObjectReference: v1.LocalObjectReference{
					Name: "additional-helm-values",
				},
			},
		}, argoappv1.ValuesFromSource{
			ConfigMapKeyRef: &v1.ConfigMapKeySelector{
				LocalObjectReference: v1.LocalObjectReference{
					Name: "more-additional-helm-values",
				},
			},
		})

		kubeclientset := fake.NewSimpleClientset(&v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testNamespace,
				Name:      "additional-helm-values",
			},
			Data: map[string]string{
				"values.yaml": "baz: quux",
			},
		}, &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testNamespace,
				Name:      "more-additional-helm-values",
			},
			Data: map[string]string{
				"values.yaml": "test: data\n",
			},
		})

		values, err := ResolveHelmValues(kubeclientset, testNamespace, spec)
		assert.Nil(t, err)
		assert.Equal(t, "baz: quux\ntest: data\n", values)
	})

	t.Run("Optional ConfigMap", func(t *testing.T) {
		const testNamespace = "argocd"
		spec := createHelmApplicationSpec()

		spec.Source.Helm.ValuesFrom = append(spec.Source.Helm.ValuesFrom, argoappv1.ValuesFromSource{
			ConfigMapKeyRef: &v1.ConfigMapKeySelector{
				Optional: &trueValue,
				LocalObjectReference: v1.LocalObjectReference{
					Name: "additional-helm-values",
				},
			},
		})

		kubeclientset := fake.NewSimpleClientset()

		values, err := ResolveHelmValues(kubeclientset, testNamespace, spec)
		assert.Nil(t, err)
		assert.Equal(t, "{}\n", values)
	})

	t.Run("Required ConfigMap", func(t *testing.T) {
		const testNamespace = "argocd"
		spec := createHelmApplicationSpec()

		spec.Source.Helm.ValuesFrom = append(spec.Source.Helm.ValuesFrom, argoappv1.ValuesFromSource{
			ConfigMapKeyRef: &v1.ConfigMapKeySelector{
				LocalObjectReference: v1.LocalObjectReference{
					Name: "additional-helm-values",
				},
			},
		})

		kubeclientset := fake.NewSimpleClientset()

		_, err := ResolveHelmValues(kubeclientset, testNamespace, spec)
		assert.Error(t, err)
	})

	t.Run("Single Secret", func(t *testing.T) {
		const testNamespace = "argocd"
		spec := createHelmApplicationSpec()

		spec.Source.Helm.ValuesFrom = append(spec.Source.Helm.ValuesFrom, argoappv1.ValuesFromSource{
			SecretKeyRef: &v1.SecretKeySelector{
				LocalObjectReference: v1.LocalObjectReference{
					Name: "additional-helm-values",
				},
			},
		})

		kubeclientset := fake.NewSimpleClientset(&v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "additional-helm-values",
				Namespace: testNamespace,
			},
			Data: map[string][]byte{
				"values.yaml": []byte("baz: 'quux'\n"),
			},
		})

		values, err := ResolveHelmValues(kubeclientset, testNamespace, spec)
		assert.Nil(t, err)
		assert.Equal(t, "baz: quux\n", values)
	})

	t.Run("Multiple Secret", func(t *testing.T) {
		const testNamespace = "argocd"
		spec := createHelmApplicationSpec()

		spec.Source.Helm.ValuesFrom = append(spec.Source.Helm.ValuesFrom, argoappv1.ValuesFromSource{
			SecretKeyRef: &v1.SecretKeySelector{
				LocalObjectReference: v1.LocalObjectReference{
					Name: "additional-helm-values",
				},
			},
		}, argoappv1.ValuesFromSource{
			SecretKeyRef: &v1.SecretKeySelector{
				LocalObjectReference: v1.LocalObjectReference{
					Name: "more-additional-helm-values",
				},
			},
		})

		kubeclientset := fake.NewSimpleClientset(&v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testNamespace,
				Name:      "additional-helm-values",
			},
			Data: map[string][]byte{
				"values.yaml": []byte("baz: 'quux'\n"),
			},
		}, &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testNamespace,
				Name:      "more-additional-helm-values",
			},
			Data: map[string][]byte{
				"values.yaml": []byte("test: data\n"),
			},
		})

		values, err := ResolveHelmValues(kubeclientset, testNamespace, spec)
		assert.Nil(t, err)
		assert.Equal(t, "baz: quux\ntest: data\n", values)
	})

	t.Run("Optional Secret", func(t *testing.T) {
		const testNamespace = "argocd"
		spec := createHelmApplicationSpec()

		spec.Source.Helm.ValuesFrom = append(spec.Source.Helm.ValuesFrom, argoappv1.ValuesFromSource{
			SecretKeyRef: &v1.SecretKeySelector{
				Optional: &trueValue,
				LocalObjectReference: v1.LocalObjectReference{
					Name: "additional-helm-values",
				},
			},
		})

		kubeclientset := fake.NewSimpleClientset()

		values, err := ResolveHelmValues(kubeclientset, testNamespace, spec)
		assert.Nil(t, err)
		assert.Equal(t, "{}\n", values)
	})

	t.Run("Required Secret", func(t *testing.T) {
		const testNamespace = "argocd"
		spec := createHelmApplicationSpec()

		spec.Source.Helm.ValuesFrom = append(spec.Source.Helm.ValuesFrom, argoappv1.ValuesFromSource{
			SecretKeyRef: &v1.SecretKeySelector{
				LocalObjectReference: v1.LocalObjectReference{
					Name: "additional-helm-values",
				},
			},
		})

		kubeclientset := fake.NewSimpleClientset()

		_, err := ResolveHelmValues(kubeclientset, testNamespace, spec)
		assert.Error(t, err)
	})
}
