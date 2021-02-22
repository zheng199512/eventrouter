/*
Copyright 2017 Heptio Inc.

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

package main

import (
	"fmt"
	"github.com/golang/glog"
	"github.com/heptiolabs/eventrouter/sinks"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/viper"
	v1 "k8s.io/api/core/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

var (
	kubernetesWarningEventGaugeVec = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "heptio_eventrouter_warnings_total",
		Help: "Total number of warning events in the kubernetes cluster",
	}, []string{
		"involved_object_kind",
		"involved_object_name",
		"involved_object_namespace",
		"reason",
		"source",
		"event_name",
	})
	kubernetesNormalEventGaugeVec = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "heptio_eventrouter_normal_total",
		Help: "Total number of normal events in the kubernetes cluster",
	}, []string{
		"involved_object_kind",
		"involved_object_name",
		"involved_object_namespace",
		"reason",
		"source",
		"event_name",
	})
	kubernetesInfoEventGaugeVec = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "heptio_eventrouter_info_total",
		Help: "Total number of info events in the kubernetes cluster",
	}, []string{
		"involved_object_kind",
		"involved_object_name",
		"involved_object_namespace",
		"reason",
		"source",
		"event_name",
	})
	kubernetesUnknownEventGaugeVec = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "heptio_eventrouter_unknown_total",
		Help: "Total number of events of unknown type in the kubernetes cluster",
	}, []string{
		"involved_object_kind",
		"involved_object_name",
		"involved_object_namespace",
		"reason",
		"source",
		"event_name",
	})
)

// EventRouter is responsible for maintaining a stream of kubernetes
// system Events and pushing them to another channel for storage
type EventRouter struct {
	// kubeclient is the main kubernetes interface
	kubeClient kubernetes.Interface

	// store of events populated by the shared informer
	eLister corelisters.EventLister

	// returns true if the event store has been synced
	eListerSynched cache.InformerSynced

	// event sink
	// TODO: Determine if we want to support multiple sinks.
	eSink sinks.EventSinkInterface
}

// NewEventRouter will create a new event router using the input params
func NewEventRouter(kubeClient kubernetes.Interface, eventsInformer coreinformers.EventInformer) *EventRouter {
	if viper.GetBool("enable-prometheus") {
		prometheus.MustRegister(kubernetesWarningEventGaugeVec)
		prometheus.MustRegister(kubernetesNormalEventGaugeVec)
		prometheus.MustRegister(kubernetesInfoEventGaugeVec)
		prometheus.MustRegister(kubernetesUnknownEventGaugeVec)
	}

	er := &EventRouter{
		kubeClient: kubeClient,
		eSink:      sinks.ManufactureSink(),
	}
	//glog.Errorf("new event router")
	eventsInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    er.addEvent,
		UpdateFunc: er.updateEvent,
		DeleteFunc: er.deleteEvent,
	})

	er.eLister = eventsInformer.Lister()
	er.eListerSynched = eventsInformer.Informer().HasSynced
	//glog.Errorf("sync ok")
	return er
}

// Run starts the EventRouter/Controller.
func (er *EventRouter) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer glog.Infof("Shutting down EventRouter")

	glog.Infof("Starting EventRouter")

	// here is where we kick the caches into gear
	if !cache.WaitForCacheSync(stopCh, er.eListerSynched) {
		utilruntime.HandleError(fmt.Errorf("timed out waiting for caches to sync"))
		return
	}
	<-stopCh
}

// addEvent is called when an event is created, or during the initial list
func (er *EventRouter) addEvent(obj interface{}) {
	e := obj.(*v1.Event)
	prometheusEvent(e, false)
	er.eSink.UpdateEvents(e, nil)
}

// updateEvent is called any time there is an update to an existing event
func (er *EventRouter) updateEvent(objOld interface{}, objNew interface{}) {
	eOld := objOld.(*v1.Event)
	eNew := objNew.(*v1.Event)
	prometheusEvent(eNew, false)
	er.eSink.UpdateEvents(eNew, eOld)
}

// prometheusEvent is called when an event is added or updated
func prometheusEvent(event *v1.Event, shouldDel bool) {
	if !viper.GetBool("enable-prometheus") {
		return
	}

	var gauge prometheus.Gauge
	var err error

	var delok bool
	if shouldDel {
		switch event.Type {
		case "Normal":
			delok = kubernetesNormalEventGaugeVec.DeleteLabelValues(
				event.InvolvedObject.Kind,
				event.InvolvedObject.Name,
				event.InvolvedObject.Namespace,
				event.Reason,
				event.Source.Host,
				event.ObjectMeta.Name,
			)
		case "Warning":
			delok = kubernetesWarningEventGaugeVec.DeleteLabelValues(
				event.InvolvedObject.Kind,
				event.InvolvedObject.Name,
				event.InvolvedObject.Namespace,
				event.Reason,
				event.Source.Host,
				event.ObjectMeta.Name,
			)
		case "Info":
			delok = kubernetesInfoEventGaugeVec.DeleteLabelValues(
				event.InvolvedObject.Kind,
				event.InvolvedObject.Name,
				event.InvolvedObject.Namespace,
				event.Reason,
				event.Source.Host,
				event.ObjectMeta.Name,
			)
		default:
			delok = kubernetesUnknownEventGaugeVec.DeleteLabelValues(
				event.InvolvedObject.Kind,
				event.InvolvedObject.Name,
				event.InvolvedObject.Namespace,
				event.Reason,
				event.Source.Host,
				event.ObjectMeta.Name,
			)
		}
		glog.Infof("result: %t del event: %s ", delok, event.ObjectMeta.Name)
		return
	}
	switch event.Type {
	case "Normal":
		gauge, err = kubernetesNormalEventGaugeVec.GetMetricWithLabelValues(
			event.InvolvedObject.Kind,
			event.InvolvedObject.Name,
			event.InvolvedObject.Namespace,
			event.Reason,
			event.Source.Host,
			event.ObjectMeta.Name,
		)
	case "Warning":
		gauge, err = kubernetesWarningEventGaugeVec.GetMetricWithLabelValues(
			event.InvolvedObject.Kind,
			event.InvolvedObject.Name,
			event.InvolvedObject.Namespace,
			event.Reason,
			event.Source.Host,
			event.ObjectMeta.Name,
		)
	case "Info":
		gauge, err = kubernetesInfoEventGaugeVec.GetMetricWithLabelValues(
			event.InvolvedObject.Kind,
			event.InvolvedObject.Name,
			event.InvolvedObject.Namespace,
			event.Reason,
			event.Source.Host,
			event.ObjectMeta.Name,
		)
	default:
		gauge, err = kubernetesUnknownEventGaugeVec.GetMetricWithLabelValues(
			event.InvolvedObject.Kind,
			event.InvolvedObject.Name,
			event.InvolvedObject.Namespace,
			event.Reason,
			event.Source.Host,
			event.ObjectMeta.Name,
		)
	}

	if err != nil {
		// Not sure this is the right place to log this error?
		glog.Warning(err)
	} else {
		gauge.Inc()
	}
}

// deleteEvent should only occur when the system garbage collects events via TTL expiration
func (er *EventRouter) deleteEvent(obj interface{}) {
	e := obj.(*v1.Event)
	prometheusEvent(e, true)
	// NOTE: This should *only* happen on TTL expiration there
	// is no reason to push this to a sink
	glog.V(5).Infof("Event Deleted from the system:\n%v", e)
}
