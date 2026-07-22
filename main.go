package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/tenzir/terraform-provider-tenzir/internal/provider"
)

// version is set at build time via -ldflags, e.g. by goreleaser.
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false,
		"set to true to run the provider with support for debuggers like delve")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		// The address the provider is published under in the Terraform registry.
		Address: "registry.terraform.io/tenzir/tenzir",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err.Error())
	}
}
