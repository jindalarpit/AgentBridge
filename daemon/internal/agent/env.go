package agent

import "os"

// getenv wraps os.Getenv for use in the default env lookup function.
var getenv = os.Getenv
