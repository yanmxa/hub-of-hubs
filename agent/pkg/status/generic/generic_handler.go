package generic

import (
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stolostron/multicluster-global-hub/agent/pkg/status/interfaces"
	genericpayload "github.com/stolostron/multicluster-global-hub/pkg/bundle/generic"
	"github.com/stolostron/multicluster-global-hub/pkg/logger"
	policiesv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
)

var log = logger.DefaultZapLogger()

type genericHandler struct {
	eventData *genericpayload.GenericObjectBundle
	// isSpec is to let the handler only update the event when spec is changed.
	// the current replicated policy event will also emit such message,it is true for policy,
	// haven't handle the other object spec like placement, appsub...
	isSpec bool

	tweakFunc    func(client.Object)
	shouldUpdate func(client.Object) bool
}

func NewGenericHandler(eventData *genericpayload.GenericObjectBundle, opts ...HandlerOption) interfaces.Handler {
	h := &genericHandler{
		eventData: eventData,
		isSpec:    false,
	}

	for _, fn := range opts {
		fn(h)
	}
	return h
}

func (h *genericHandler) Get() interface{} {
	return h.eventData
}

func (h *genericHandler) Update(obj client.Object) bool {
	// if obj is instance policiesv1.Policy, we need to check if it is a root policy, assert it
	isPolicy := false
	if policy, ok := obj.(*policiesv1.Policy); ok {
		isPolicy = true
		log.Infof("updating the policy %s/%s into bundle %v", policy.GetNamespace(), policy.GetName(), *h.eventData)
	}

	defer func() {
		if isPolicy {
			log.Infof("updated the policy %s/%s into bundle %v", obj.GetNamespace(), obj.GetName(), *h.eventData)
		}
	}()

	if h.shouldUpdate != nil {
		if updated := h.shouldUpdate(obj); !updated {
			return false
		}
	}

	index := getObjectIndexByUID(obj.GetUID(), (*h.eventData))
	if index == -1 { // object not found, need to add it to the bundle
		(*h.eventData) = append((*h.eventData), obj)
		return true
	}

	old := (*h.eventData)[index]
	if h.isSpec && old.GetGeneration() == obj.GetGeneration() {
		log.Infof("skipping update for %s/%s, generation is the same: %d", obj.GetNamespace(), obj.GetName(), obj.GetGeneration())
		return false
	}

	// if we reached here, object already exists in the bundle. check if we need to update the object
	if obj.GetResourceVersion() == (*h.eventData)[index].GetResourceVersion() {
		log.Infof("skipping update for %s/%s, resourceVersion is the same: %d", obj.GetNamespace(), obj.GetName(), obj.GetGeneration())
		return false // update in bundle only if object changed. check for changes using resourceVersion field
	}

	(*h.eventData)[index] = obj

	// tweak
	if h.tweakFunc != nil {
		h.tweakFunc(obj)
	}
	return true
}

func (h *genericHandler) Delete(obj client.Object) bool {
	log.Infof("deleting the object %s/%s from bundle %v", obj.GetNamespace(), obj.GetName(), *h.eventData)
	if h.shouldUpdate != nil {
		if updated := h.shouldUpdate(obj); !updated {
			return false
		}
	}

	index := getObjectIndexByObj(obj, (*h.eventData))
	if index == -1 { // trying to delete object which doesn't exist
		return false
	}

	(*h.eventData) = append((*h.eventData)[:index], (*h.eventData)[index+1:]...) // remove from objects
	log.Infof("deleted the object %s/%s from bundle %v", obj.GetNamespace(), obj.GetName(), *h.eventData)
	return true
}

func getObjectIndexByUID(uid types.UID, objects []client.Object) int {
	for i, object := range objects {
		if object.GetUID() == uid {
			return i
		}
	}
	return -1
}

func getObjectIndexByObj(obj client.Object, objects []client.Object) int {
	if len(obj.GetUID()) > 0 {
		return getObjectIndexByUID(obj.GetUID(), objects)
	} else {
		for i, object := range objects {
			if object.GetNamespace() == obj.GetNamespace() && object.GetName() == obj.GetName() {
				return i
			}
		}
	}
	return -1
}

// define the emitter options
type HandlerOption func(*genericHandler)

func WithTweakFunc(tweakFunc func(client.Object)) HandlerOption {
	return func(g *genericHandler) {
		g.tweakFunc = tweakFunc
	}
}

func WithSpec(onlySpec bool) HandlerOption {
	return func(g *genericHandler) {
		g.isSpec = onlySpec
	}
}

func WithShouldUpdate(shouldUpdate func(client.Object) bool) HandlerOption {
	return func(g *genericHandler) {
		g.shouldUpdate = shouldUpdate
	}
}
