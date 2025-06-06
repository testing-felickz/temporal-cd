// Code generated by gowrap. DO NOT EDIT.
// template: gowrap_template
// gowrap: http://github.com/hexdigest/gowrap

package faultinjection

//go:generate gowrap gen -p go.temporal.io/server/common/persistence -i MetadataStore -t gowrap_template -o metadata_store_gen.go -l ""

import (
	"context"

	_sourcePersistence "go.temporal.io/server/common/persistence"
)

type (
	// faultInjectionMetadataStore implements MetadataStore interface with fault injection.
	faultInjectionMetadataStore struct {
		_sourcePersistence.MetadataStore
		generator faultGenerator
	}
)

// newFaultInjectionMetadataStore returns faultInjectionMetadataStore.
func newFaultInjectionMetadataStore(
	baseStore _sourcePersistence.MetadataStore,
	generator faultGenerator,
) *faultInjectionMetadataStore {
	return &faultInjectionMetadataStore{
		MetadataStore: baseStore,
		generator:     generator,
	}
}

// CreateNamespace wraps MetadataStore.CreateNamespace.
func (d faultInjectionMetadataStore) CreateNamespace(ctx context.Context, request *_sourcePersistence.InternalCreateNamespaceRequest) (cp1 *_sourcePersistence.CreateNamespaceResponse, err error) {
	err = d.generator.generate("CreateNamespace").inject(func() error {
		cp1, err = d.MetadataStore.CreateNamespace(ctx, request)
		return err
	})
	return
}

// DeleteNamespace wraps MetadataStore.DeleteNamespace.
func (d faultInjectionMetadataStore) DeleteNamespace(ctx context.Context, request *_sourcePersistence.DeleteNamespaceRequest) (err error) {
	err = d.generator.generate("DeleteNamespace").inject(func() error {
		err = d.MetadataStore.DeleteNamespace(ctx, request)
		return err
	})
	return
}

// DeleteNamespaceByName wraps MetadataStore.DeleteNamespaceByName.
func (d faultInjectionMetadataStore) DeleteNamespaceByName(ctx context.Context, request *_sourcePersistence.DeleteNamespaceByNameRequest) (err error) {
	err = d.generator.generate("DeleteNamespaceByName").inject(func() error {
		err = d.MetadataStore.DeleteNamespaceByName(ctx, request)
		return err
	})
	return
}

// GetNamespace wraps MetadataStore.GetNamespace.
func (d faultInjectionMetadataStore) GetNamespace(ctx context.Context, request *_sourcePersistence.GetNamespaceRequest) (ip1 *_sourcePersistence.InternalGetNamespaceResponse, err error) {
	err = d.generator.generate("GetNamespace").inject(func() error {
		ip1, err = d.MetadataStore.GetNamespace(ctx, request)
		return err
	})
	return
}

// ListNamespaces wraps MetadataStore.ListNamespaces.
func (d faultInjectionMetadataStore) ListNamespaces(ctx context.Context, request *_sourcePersistence.InternalListNamespacesRequest) (ip1 *_sourcePersistence.InternalListNamespacesResponse, err error) {
	err = d.generator.generate("ListNamespaces").inject(func() error {
		ip1, err = d.MetadataStore.ListNamespaces(ctx, request)
		return err
	})
	return
}

// RenameNamespace wraps MetadataStore.RenameNamespace.
func (d faultInjectionMetadataStore) RenameNamespace(ctx context.Context, request *_sourcePersistence.InternalRenameNamespaceRequest) (err error) {
	err = d.generator.generate("RenameNamespace").inject(func() error {
		err = d.MetadataStore.RenameNamespace(ctx, request)
		return err
	})
	return
}

// UpdateNamespace wraps MetadataStore.UpdateNamespace.
func (d faultInjectionMetadataStore) UpdateNamespace(ctx context.Context, request *_sourcePersistence.InternalUpdateNamespaceRequest) (err error) {
	err = d.generator.generate("UpdateNamespace").inject(func() error {
		err = d.MetadataStore.UpdateNamespace(ctx, request)
		return err
	})
	return
}
