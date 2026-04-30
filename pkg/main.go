package main

import (
	"os"

	"github.com/grafana/grafana-plugin-sdk-go/backend/datasource"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"

	"github.com/firestoned/grafana-splunk-datasource/pkg/plugin"
)

func main() {
	if err := datasource.Manage(
		"firestoned-splunk-datasource",
		plugin.NewDatasource,
		datasource.ManageOpts{},
	); err != nil {
		log.DefaultLogger.Error("plugin server exited", "err", err.Error())
		os.Exit(1)
	}
}
