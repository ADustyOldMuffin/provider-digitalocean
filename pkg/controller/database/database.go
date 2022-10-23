/*
Copyright 2021 The Crossplane Authors.
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

package database

import (
	"context"

	"github.com/digitalocean/godo"
	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/pkg/resource"

	"github.com/crossplane-contrib/provider-digitalocean/apis/database/v1alpha1"
	do "github.com/crossplane-contrib/provider-digitalocean/pkg/clients"
	dodb "github.com/crossplane-contrib/provider-digitalocean/pkg/clients/database"
)

const (
	// Error strings.
	errNotDB          = "managed resource is not a Database Cluster resource"
	errGetDB          = "cannot get a Database Cluster"
	errDBNameRequired = "name of Database Cluster is required"

	errDBCreateFailed = "creation of Database Cluster resource has failed"
	errDBDeleteFailed = "deletion of Database Cluster resource has failed"
	errDBUpdate       = "cannot update managed Database Cluster resource"
)

// SetupDatabase adds a controller that reconciles Database managed
// resources.
func SetupDatabase(mgr ctrl.Manager, l logging.Logger) error {
	name := managed.ControllerName(v1alpha1.DBGroupKind)

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		For(&v1alpha1.DODatabaseCluster{}).
		Complete(managed.NewReconciler(mgr,
			resource.ManagedKind(v1alpha1.DBGroupVersionKind),
			managed.WithExternalConnecter(&dbConnector{kube: mgr.GetClient()}),
			managed.WithReferenceResolver(managed.NewAPISimpleReferenceResolver(mgr.GetClient())),
			managed.WithInitializers(managed.NewDefaultProviderConfig(mgr.GetClient())),
			managed.WithLogger(l.WithValues("controller", name)),
			managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorderFor(name)))))
}

type dbConnector struct {
	kube client.Client
}

func (c *dbConnector) Connect(ctx context.Context, mg resource.Managed) (managed.ExternalClient, error) {
	token, err := do.GetAuthInfo(ctx, c.kube, mg)
	if err != nil {
		return nil, err
	}
	client := godo.NewFromToken(token)
	return &dbExternal{Client: client, kube: c.kube}, nil
}

type dbExternal struct {
	kube client.Client
	*godo.Client
}

func (c *dbExternal) Observe(ctx context.Context, mg resource.Managed) (managed.ExternalObservation, error) {
	cr, ok := mg.(*v1alpha1.DODatabaseCluster)
	if !ok {
		return managed.ExternalObservation{}, errors.New(errNotDB)
	}

	en := meta.GetExternalName(cr)

	if en == "" {
		return managed.ExternalObservation{
			ResourceExists: false,
		}, nil
	}

	observed, response, err := c.Databases.Get(ctx, meta.GetExternalName(cr))
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(do.IgnoreNotFound(err, response), errGetDB)
	}

	currentSpec := cr.Spec.ForProvider.DeepCopy()
	dodb.LateInitializeSpec(&cr.Spec.ForProvider, *observed)
	if !cmp.Equal(currentSpec, &cr.Spec.ForProvider) {
		if err := c.kube.Update(ctx, cr); err != nil {
			return managed.ExternalObservation{}, errors.Wrap(err, errDBUpdate)
		}
	}

	cr.Status.AtProvider = dodb.GenerateObservation(observed)

	setCrossplaneStatus(cr)

	return managed.ExternalObservation{
		ResourceExists:   true,
		ResourceUpToDate: true,
	}, nil
}

func setCrossplaneStatus(cr *v1alpha1.DODatabaseCluster) {
	switch cr.Status.AtProvider.Status {
	case v1alpha1.StatusCreating:
		cr.SetConditions(xpv1.Creating())
	case v1alpha1.StatusOnline:
		cr.SetConditions(xpv1.Available())
	case v1alpha1.StatusMigrating:
	case v1alpha1.StatusResizing:
	case v1alpha1.StatusForking:
		cr.SetConditions(xpv1.Unavailable())
	}
}

func (c *dbExternal) Create(ctx context.Context, mg resource.Managed) (managed.ExternalCreation, error) {
	cr, ok := mg.(*v1alpha1.DODatabaseCluster)
	if !ok {
		return managed.ExternalCreation{}, errors.New(errNotDB)
	}

	cr.Status.SetConditions(xpv1.Creating())

	create := &godo.DatabaseCreateRequest{}

	name := ""
	if meta.GetExternalName(cr) != "" {
		name = meta.GetExternalName(cr)
	} else {
		name = cr.GetName()
	}

	if name == "" {
		return managed.ExternalCreation{}, errors.New(errDBNameRequired)
	}

	dodb.GenerateDatabase(name, cr.Spec.ForProvider, create)

	db, _, err := c.Databases.Create(ctx, create)
	if err != nil || db == nil {
		return managed.ExternalCreation{}, errors.Wrap(err, errDBCreateFailed)
	}

	meta.SetExternalName(cr, db.ID)

	ec := managed.ExternalCreation{}

	if cr.Spec.WriteConnectionSecretToReference != nil {
		ec.ConnectionDetails = dodb.GenerateConnectionDetails(ctx, db, c.Client)
	}

	return ec, nil
}

func (c *dbExternal) Update(ctx context.Context, mg resource.Managed) (managed.ExternalUpdate, error) {
	// We don't allow the updating of databases right now.
	return managed.ExternalUpdate{}, nil
}

func (c *dbExternal) Delete(ctx context.Context, mg resource.Managed) error {
	cr, ok := mg.(*v1alpha1.DODatabaseCluster)
	if !ok {
		return errors.New(errNotDB)
	}

	cr.Status.SetConditions(xpv1.Deleting())

	response, err := c.Databases.Delete(ctx, *cr.Status.AtProvider.ID)
	return errors.Wrap(do.IgnoreNotFound(err, response), errDBDeleteFailed)
}
