// Command conduit opens an AWS SSM port-forwarding session.
package main

import (
	"log"

	"conduit/cli"
	"conduit/internal/conduit"
)

func main() {
	if err := cli.NewRootCmd(conduit.Run).Execute(); err != nil {
		log.Fatal(err)
	}
}
