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
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
	v1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"
)

// FakeWorks implements WorkInterface
type FakeWorks struct {
	Fake *FakeMulticlusterV1alpha1
	ns   string
}

var worksResource = v1alpha1.SchemeGroupVersion.WithResource("works")

var worksKind = v1alpha1.SchemeGroupVersion.WithKind("Work")

// Get takes name of the work, and returns the corresponding work object, and an error if there is any.
func (c *FakeWorks) Get(ctx context.Context, name string, options v1.GetOptions) (result *v1alpha1.Work, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(worksResource, c.ns, name), &v1alpha1.Work{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.Work), err
}

// List takes label and field selectors, and returns the list of Works that match those selectors.
func (c *FakeWorks) List(ctx context.Context, opts v1.ListOptions) (result *v1alpha1.WorkList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(worksResource, worksKind, c.ns, opts), &v1alpha1.WorkList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v1alpha1.WorkList{ListMeta: obj.(*v1alpha1.WorkList).ListMeta}
	for _, item := range obj.(*v1alpha1.WorkList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested works.
func (c *FakeWorks) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(worksResource, c.ns, opts))

}

// Create takes the representation of a work and creates it.  Returns the server's representation of the work, and an error, if there is any.
func (c *FakeWorks) Create(ctx context.Context, work *v1alpha1.Work, opts v1.CreateOptions) (result *v1alpha1.Work, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(worksResource, c.ns, work), &v1alpha1.Work{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.Work), err
}

// Update takes the representation of a work and updates it. Returns the server's representation of the work, and an error, if there is any.
func (c *FakeWorks) Update(ctx context.Context, work *v1alpha1.Work, opts v1.UpdateOptions) (result *v1alpha1.Work, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(worksResource, c.ns, work), &v1alpha1.Work{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.Work), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeWorks) UpdateStatus(ctx context.Context, work *v1alpha1.Work, opts v1.UpdateOptions) (*v1alpha1.Work, error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateSubresourceAction(worksResource, "status", c.ns, work), &v1alpha1.Work{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.Work), err
}

// Delete takes name of the work and deletes it. Returns an error if one occurs.
func (c *FakeWorks) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteActionWithOptions(worksResource, c.ns, name, opts), &v1alpha1.Work{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeWorks) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(worksResource, c.ns, listOpts)

	_, err := c.Fake.Invokes(action, &v1alpha1.WorkList{})
	return err
}

// Patch applies the patch and returns the patched work.
func (c *FakeWorks) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.Work, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(worksResource, c.ns, name, pt, data, subresources...), &v1alpha1.Work{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.Work), err
}
