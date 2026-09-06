package main

const (
	diagnosticOK                     = "OK"
	diagnosticProviderUnreachable    = "PROVIDER_UNREACHABLE"
	diagnosticInstanceMissing        = "INSTANCE_MISSING"
	diagnosticInstanceNotRunning     = "INSTANCE_NOT_RUNNING"
	diagnosticSSHMetadataMissing     = "SSH_METADATA_MISSING"
	diagnosticSSHCommandFailed       = "SSH_COMMAND_FAILED"
	diagnosticRuntimeBinaryMissing   = "RUNTIME_BINARY_MISSING"
	diagnosticRuntimeProcessDead     = "RUNTIME_PROCESS_DEAD"
	diagnosticRuntimeProcessStarting = "RUNTIME_PROCESS_STARTING"
	diagnosticModelDownloading       = "MODEL_DOWNLOADING"
	diagnosticRemotePortNotListening = "REMOTE_PORT_NOT_LISTENING"
	diagnosticTunnelProcessMissing   = "TUNNEL_PROCESS_MISSING"
	diagnosticLocalPortNotListening  = "LOCAL_PORT_NOT_LISTENING"
	diagnosticLocalEndpointRefused   = "LOCAL_ENDPOINT_REFUSED"
	diagnosticLocalEndpointTimeout   = "LOCAL_ENDPOINT_TIMEOUT"
	diagnosticWatchdogMissing        = "WATCHDOG_MISSING"
	diagnosticDestroyUnconfirmed     = "DESTROY_UNCONFIRMED"
)
