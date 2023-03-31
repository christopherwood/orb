/*
Copyright SecureKey Technologies Inc. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package startcmd

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/trustbloc/logutil-go/pkg/log"
)

const testLogModuleName = "test"

var testLogger = log.New(testLogModuleName)

func TestSetLogLevel(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		defer resetLoggingLevels()

		setLogLevels(testLogger, "debug")

		require.Equal(t, log.DEBUG, log.GetLevel(""))
	})

	t.Run("Log spec -> Success", func(t *testing.T) {
		defer resetLoggingLevels()

		setLogLevels(testLogger, "module1=debug:module2=error:warning")

		require.Equal(t, log.WARNING, log.GetLevel(""))
		require.Equal(t, log.DEBUG, log.GetLevel("module1"))
		require.Equal(t, log.ERROR, log.GetLevel("module2"))
	})

	t.Run("Invalid log level", func(t *testing.T) {
		defer resetLoggingLevels()

		setLogLevels(testLogger, "mango")

		// Should remain unchanged
		require.Equal(t, log.INFO, log.GetLevel(""))
	})
}

func resetLoggingLevels() {
	log.SetDefaultLevel(log.INFO)
	log.SetLevel("module1", log.INFO)
	log.SetLevel("module2", log.INFO)
	log.SetLevel("module3", log.INFO)
}
