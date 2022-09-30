/*
 * Copyright (c) 2022, Salesforce, Inc.
 * All rights reserved.
 * SPDX-License-Identifier: BSD-3-Clause
 * For full license text, see the LICENSE file in the repo root or https://opensource.org/licenses/BSD-3-Clause
 */

package livenessprobe

import (
	"fmt"
	"github.com/go-logr/logr"
	"net/http"
	"time"
)

var getTimeFunc = time.Now

type HeartbeatRegister interface {
	Pulse()
}

type LivenessProbeListener struct {
	path             string
	port             string
	log              logr.Logger
	heartbeatTimeout time.Duration
	lastReportTime   time.Time
}

func NewLivenessProbeListener(path string, port string, heartbeatTimeout time.Duration, log logr.Logger) *LivenessProbeListener {
	return &LivenessProbeListener{
		heartbeatTimeout: heartbeatTimeout,
		lastReportTime:   getTimeFunc(),
		path:             path,
		port:             port,
		log:              log,
	}
}

func (l *LivenessProbeListener) StartLivenessProbeListener() {
	mux := http.NewServeMux()
	l.log.Info("Starting LivenessProbeListener", "port", l.port)
	mux.HandleFunc(l.path, l.respondToProbeWrapper)
	err := http.ListenAndServe(":"+l.port, logWrapper(mux, l.log))

	panic(fmt.Sprintf("Liveness probe listener died because %v.", err))
}

func (l *LivenessProbeListener) respondToProbeWrapper(w http.ResponseWriter, r *http.Request) {
	localLastReportTime := l.lastReportTime
	l.log.Info("Checking health now", "Probe time", getTimeFunc().Sub(localLastReportTime), "Was last reported healthy", localLastReportTime)
	if getTimeFunc().Sub(localLastReportTime) > l.heartbeatTimeout {
		reportControllerError(w, fmt.Sprintf("Controller has stopped reporting status, last update at %v", localLastReportTime))
	} else {
		reportOk(w)
	}
}

func logWrapper(handler http.Handler, log logr.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userAgent := r.Header.Get("User-Agent")
		log.Info("Http Request", "Address", r.RemoteAddr, "User Agent", userAgent, "Method", r.Method, "URL", r.URL)
		handler.ServeHTTP(w, r)
	})
}

func (l *LivenessProbeListener) Pulse() {
	l.lastReportTime = getTimeFunc()
	l.log.Info("Pulse recorded", "Pulse time", l.lastReportTime)
}

func reportControllerError(w http.ResponseWriter, error string) {
	w.WriteHeader(http.StatusInternalServerError)
	w.Write([]byte(error))
}

func reportOk(w http.ResponseWriter) {
	w.WriteHeader(http.StatusOK)
}
