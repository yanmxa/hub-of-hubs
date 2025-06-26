package generic

import (
	cloudevents "github.com/cloudevents/sdk-go/v2"

	"github.com/stolostron/multicluster-global-hub/agent/pkg/configs"
	specsyncers "github.com/stolostron/multicluster-global-hub/agent/pkg/spec/syncers"
	"github.com/stolostron/multicluster-global-hub/agent/pkg/status/interfaces"
	eventversion "github.com/stolostron/multicluster-global-hub/pkg/bundle/version"
	"github.com/stolostron/multicluster-global-hub/pkg/enum"
	"github.com/stolostron/multicluster-global-hub/pkg/utils"
)

var _ interfaces.Emitter = &genericEmitter{}

type genericEmitter struct {
	eventType       enum.EventType
	currentVersion  *eventversion.Version
	lastSentVersion eventversion.Version

	topic             string
	dependencyVersion *eventversion.Version

	postSend func(interface{})
}

func NewGenericEmitter(
	eventType enum.EventType,
	opts ...EmitterOption,
) *genericEmitter {
	emitter := &genericEmitter{
		eventType:      eventType,
		currentVersion: eventversion.NewVersion(),
		// lastSentVersion: *eventversion.NewVersion(),
	}
	emitter.applyOptions(opts...)
	emitter.lastSentVersion = *emitter.currentVersion
	// support resync
	specsyncers.EnableResync(string(eventType), emitter.currentVersion)
	return emitter
}

func (e *genericEmitter) applyOptions(opts ...EmitterOption) {
	for _, fn := range opts {
		fn(e)
	}
}

func (h *genericEmitter) ShouldSend() bool {
	if h.eventType == enum.LocalPolicySpecType {
		log.Infof("sending the local policy condition: %s, current version: %s, last sent version: %s", h.currentVersion.NewerThan(&h.lastSentVersion), h.currentVersion.String(), h.lastSentVersion.String())
	}
	return h.currentVersion.NewerThan(&h.lastSentVersion)
}

func (h *genericEmitter) PostSend(data interface{}) {
	if h.postSend != nil {
		h.postSend(data)
	}
	h.currentVersion.Next()
	h.lastSentVersion = *h.currentVersion
}

func (h *genericEmitter) Topic() string {
	return h.topic
}

func (h *genericEmitter) PostUpdate() {
	h.currentVersion.Incr()
}

func (g *genericEmitter) ToCloudEvent(payload interface{}) (*cloudevents.Event, error) {
	e := cloudevents.NewEvent()
	e.SetSource(configs.GetLeafHubName())
	e.SetType(string(g.eventType))
	e.SetExtension(eventversion.ExtVersion, g.currentVersion.String())
	if g.dependencyVersion != nil {
		e.SetExtension(eventversion.ExtDependencyVersion, g.dependencyVersion.String())
	}
	err := e.SetData(cloudevents.ApplicationJSON, payload)
	if err != nil {
		log.Error(err, "failed to set data for CloudEvent", "eventType", g.eventType, "payload", payload)
		return nil, err
	}
	if g.eventType == enum.LocalPolicySpecType {
		log.Info("generating CloudEvent for local policy spec")
		utils.PrettyPrint(e)
	}
	return &e, err
}

// define the emitter options
type EmitterOption func(*genericEmitter)

func WithTopic(topic string) EmitterOption {
	return func(g *genericEmitter) {
		g.topic = topic
	}
}

func WithDependencyVersion(version *eventversion.Version) EmitterOption {
	return func(g *genericEmitter) {
		g.dependencyVersion = version
	}
}

func WithVersion(version *eventversion.Version) EmitterOption {
	return func(g *genericEmitter) {
		g.currentVersion = version
	}
}

func WithPostSend(postSend func(interface{})) EmitterOption {
	return func(g *genericEmitter) {
		g.postSend = postSend
	}
}
