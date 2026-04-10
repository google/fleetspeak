//go:build darwin
// +build darwin

// Package darkwake provides utilities for handling macOS dark wake events.
package darkwake

/*
#cgo CFLAGS: -fblocks
#cgo LDFLAGS: -framework Foundation
#include <xpc/xpc.h>
#include <xpc/activity.h>
#include <unistd.h>
#include <os/log.h>

#define XPC_ACTIVITY_REQUIRE_NETWORK_CONNECTIVITY "RequireNetworkConnectivity"
extern void TriggerDarkWakeFlush();

static void register_xpc_activity() {
    // Because launchd owns the criteria now, we use XPC_ACTIVITY_CHECK_IN
    xpc_activity_register("com.google.fleetspeak.darkwake", XPC_ACTIVITY_CHECK_IN, ^(xpc_activity_t activity) {

        xpc_activity_state_t state = xpc_activity_get_state(activity);

        // ALWAYS check the state. XPC will fire this block immediately on startup
        // with STATE_CHECK_IN. We only want to flush on STATE_RUN.
        if (state == XPC_ACTIVITY_STATE_RUN) {
            os_log(OS_LOG_DEFAULT, "Fleetspeak Dark Wake: triggering communicator flush");

            xpc_activity_set_state(activity, XPC_ACTIVITY_STATE_CONTINUE);

            // Trigger the blocking Go flush
            TriggerDarkWakeFlush();

            xpc_activity_set_state(activity, XPC_ACTIVITY_STATE_DONE);
            os_log(OS_LOG_DEFAULT, "Fleetspeak Dark Wake XPC Activity finished");
        }
    });
}
*/
import "C"
import (
	"time"
)

//export TriggerDarkWakeFlush
func TriggerDarkWakeFlush() {
	// Directly pause this goroutine for 10 seconds.
	// This is non-blocking for the rest of the application.
	time.Sleep(10 * time.Second)
}

// RegisterXPC registers the XPC activity for the fleetspeak client.
func RegisterXPC() {
	C.register_xpc_activity()
}
