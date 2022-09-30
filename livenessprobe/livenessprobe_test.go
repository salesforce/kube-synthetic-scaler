//go:build unit
// +build unit

/*
 * Copyright (c) 2022, Salesforce, Inc.
 * All rights reserved.
 * SPDX-License-Identifier: BSD-3-Clause
 * For full license text, see the LICENSE file in the repo root or https://opensource.org/licenses/BSD-3-Clause
 */

package livenessprobe

import (
	"net/http/httptest"
	"testing"
	"time"

	"net/http"

	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
)

var staticTime = time.Date(1, 1, 1, 1, 1, 1, 1, time.UTC)
var oneHourLater = staticTime.Add(time.Hour * 1)

func Test_Pulse_UpdatesMostRecentPulse(t *testing.T) {
	getTimeFunc = func() time.Time { return staticTime }
	listener := NewLivenessProbeListener("some path", "1234", time.Second*1, testr.New(t))
	getTimeFunc = func() time.Time { return oneHourLater }
	listener.Pulse()
	assert.Equal(t, oneHourLater, listener.lastReportTime)
}

func Test_RespondToProbeWrapper_ReturnsOk(t *testing.T) {
	getTimeFunc = func() time.Time { return staticTime }
	listener := NewLivenessProbeListener("/healthz", "9001", time.Hour*5, testr.New(t))
	getTimeFunc = func() time.Time { return oneHourLater }
	w := httptest.NewRecorder()

	listener.respondToProbeWrapper(w, nil)
	response := w.Result()
	assert.Equal(t, http.StatusOK, response.StatusCode)
}

func Test_RespondToProbeWrapper_ReturnsError(t *testing.T) {
	getTimeFunc = func() time.Time { return staticTime }
	listener := NewLivenessProbeListener("some path", "1234", time.Second*1, testr.New(t))
	getTimeFunc = func() time.Time { return oneHourLater }
	w := httptest.NewRecorder()

	listener.respondToProbeWrapper(w, nil)
	response := w.Result()

	assert.Equal(t, http.StatusInternalServerError, response.StatusCode)
}
