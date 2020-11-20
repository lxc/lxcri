package specs

// Copy of ContainerState type from github.com/opencontainers/runtime-spec.
// Can be removed once next OCI spec is released.

// ContainerState represents the state of a container.
type ContainerState string

const (
	// StateCreating indicates that the container is being created
	StateCreating ContainerState = "creating"

	// StateCreated indicates that the runtime has finished the create operation
	StateCreated ContainerState = "created"

	// StateRunning indicates that the container process has executed the
	// user-specified program but has not exited
	StateRunning ContainerState = "running"

	// StateStopped indicates that the container process has exited
	StateStopped ContainerState = "stopped"
)
