/*
 * Copyright 2020 The Knative Authors
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package prober

import (
	"testing"
	"time"

	"github.com/wavesoftware/go-ensure"
	"go.uber.org/zap"
	"knative.dev/eventing/test/lib"
	"knative.dev/eventing/test/lib/resources"
)

var (
	// FIXME: Interval is set to 200 msec, as lower values will result in errors: knative/eventing#2357
	// Interval = 10 * time.Millisecond
	Interval = 200 * time.Millisecond
)

// Prober is the interface for a prober, which checks the result of the probes when stopped.
type Prober interface {
	// Verify will verify prober state after finished has been send
	Verify() ([]error, int)

	// Finish send finished event
	Finish()

	// ReportError will reports found errors in proper way
	ReportError(t *testing.T, err error)

	// deploy a prober to a cluster
	deploy()
	// remove a prober from cluster
	remove()
}

// Config represents a configuration for prober
type Config struct {
	Namespace           string
	Interval            time.Duration
	Serving             ServingConfig
	FinishedSleep       time.Duration
	FailOnMissingEvents bool
}

type ServingConfig struct {
	Use         bool
	ScaleToZero bool
}

func NewConfig(namespace string) *Config {
	return &Config{
		Namespace:           namespace,
		Interval:            Interval,
		FinishedSleep:       5 * time.Second,
		FailOnMissingEvents: true,
		Serving: ServingConfig{
			Use:         false,
			ScaleToZero: true,
		},
	}
}

// RunEventProber starts a single Prober of the given domain.
func RunEventProber(log *zap.SugaredLogger, client *lib.Client, config *Config) Prober {
	pm := newProber(log, client, config)
	pm.deploy()
	return pm
}

// AssertEventProber will send finish event and then verify if all events propagated well
func AssertEventProber(t *testing.T, prober Prober) {
	prober.Finish()

	waitAfterFinished(prober)

	errors, events := prober.Verify()
	if len(errors) == 0 {
		t.Logf("All %d events propagated well", events)
	} else {
		t.Logf("There ware %v errors. Listing them below.", len(errors))
	}
	for _, err := range errors {
		prober.ReportError(t, err)
	}

	prober.remove()
}

type prober struct {
	log    *zap.SugaredLogger
	client *lib.Client
	config *Config
}

func (p *prober) servingClient() resources.ServingClient {
	return resources.ServingClient{
		Kube:    p.client.Kube,
		Dynamic: p.client.Dynamic,
	}
}

func (p *prober) ReportError(t *testing.T, err error) {
	if p.config.FailOnMissingEvents {
		t.Error(err)
	} else {
		p.log.Warnf("Silenced FAIL: %v", err)
	}
}

func (p *prober) deploy() {
	p.log.Infof("Using namespace for probe testing: %v", p.client.Namespace)
	p.deployConfiguration()
	p.deployReceiver()
	if p.config.Serving.Use {
		p.deployForwarder()
	}
	ensure.NoError(lib.AwaitForAll(p.log))

	p.deploySender()
	ensure.NoError(lib.AwaitForAll(p.log))
	// allow sender to send at least some events, 2 sec wait
	time.Sleep(2 * time.Second)
	p.log.Infof("Prober is now sending events with interval of %v in "+
		"namespace: %v", p.config.Interval, p.client.Namespace)
}

func (p *prober) remove() {
	if p.config.Serving.Use {
		p.removeForwarder()
	}
	p.removeReceiver()
	p.removeConfiguration()
}

func newProber(log *zap.SugaredLogger, client *lib.Client, config *Config) Prober {
	return &prober{
		log:    log,
		client: client,
		config: config,
	}
}

func waitAfterFinished(p Prober) {
	s := p.(*prober)
	cfg := s.config
	s.log.Infof("Waiting %v after sender finished...", cfg.FinishedSleep)
	time.Sleep(cfg.FinishedSleep)
}
