// Command container_ids verifies the runtime identity without a shell in the image.
package main

import (
	"fmt"
	"os"
)

func main() {
	if os.Geteuid() != 25000 || os.Getegid() != 25000 {
		fmt.Fprintf(os.Stderr, "unexpected runtime identity: %d:%d\n", os.Geteuid(), os.Getegid())
		os.Exit(1)
	}
	fmt.Println("runtime identity: 25000:25000")
}
