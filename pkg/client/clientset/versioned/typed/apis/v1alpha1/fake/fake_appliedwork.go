/*
Copyright 2021 The Kubernetes Authors.

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

// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	"context"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
	v1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"
)

// FakeAppliedWorks implements AppliedWorkInterface
type FakeAppliedWorks struct {
	Fake *FakeMulticlusterV1alpha1
}

var appliedworksResource = schema.GroupVersionResource{Group: "multicluster.x-k8s.io", Version: "v1alpha1", Resource: "appliedworks"}

var appliedworksKind = schema.GroupVersionKind{Group: "multicluster.x-k8s.io", Version: "v1alpha1", Kind: "AppliedWork"}

// Get takes name of the appliedWork, and returns the corresponding appliedWork object, and an error if there is any.
func (c *FakeAppliedWorks) Get(ctx context.Context, name string, options v1.GetOptions) (result *v1alpha1.AppliedWork, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootGetAction(appliedworksResource, name), &v1alpha1.AppliedWork{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.AppliedWork), err
}

// List takes label and field selectors, and returns the list of AppliedWorks that match those selectors.
func (c *FakeAppliedWorks) List(ctx context.Context, opts v1.ListOptions) (result *v1alpha1.AppliedWorkList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootListAction(appliedworksResource, appliedworksKind, opts), &v1alpha1.AppliedWorkList{})
	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v1alpha1.AppliedWorkList{ListMeta: obj.(*v1alpha1.AppliedWorkList).ListMeta}
	for _, item := range obj.(*v1alpha1.AppliedWorkList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested appliedWorks.
func (c *FakeAppliedWorks) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewRootWatchAction(appliedworksResource, opts))
}

// Create takes the representation of a appliedWork and creates it.  Returns the server's representation of the appliedWork, and an error, if there is any.
func (c *FakeAppliedWorks) Create(ctx context.Context, appliedWork *v1alpha1.AppliedWork, opts v1.CreateOptions) (result *v1alpha1.AppliedWork, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootCreateAction(appliedworksResource, appliedWork), &v1alpha1.AppliedWork{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.AppliedWork), err
}

// Update takes the representation of a appliedWork and updates it. Returns the server's representation of the appliedWork, and an error, if there is any.
func (c *FakeAppliedWorks) Update(ctx context.Context, appliedWork *v1alpha1.AppliedWork, opts v1.UpdateOptions) (result *v1alpha1.AppliedWork, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootUpdateAction(appliedworksResource, appliedWork), &v1alpha1.AppliedWork{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.AppliedWork), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeAppliedWorks) UpdateStatus(ctx context.Context, appliedWork *v1alpha1.AppliedWork, opts v1.UpdateOptions) (*v1alpha1.AppliedWork, error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootUpdateSubresourceAction(appliedworksResource, "status", appliedWork), &v1alpha1.AppliedWork{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.AppliedWork), err
}

// Delete takes name of the appliedWork and deletes it. Returns an error if one occurs.
func (c *FakeAppliedWorks) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewRootDeleteActionWithOptions(appliedworksResource, name, opts), &v1alpha1.AppliedWork{})
	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeAppliedWorks) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	action := testing.NewRootDeleteCollectionAction(appliedworksResource, listOpts)

	_, err := c.Fake.Invokes(action, &v1alpha1.AppliedWorkList{})
	return err
}

// Patch applies the patch and returns the patched appliedWork.
func (c *FakeAppliedWorks) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.AppliedWork, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootPatchSubresourceAction(appliedworksResource, name, pt, data, subresources...), &v1alpha1.AppliedWork{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.AppliedWork), err
}
