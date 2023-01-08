//go:build darwin && !skipnative

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/ripta/rt/pkg/location"
	"github.com/spf13/cobra"
)

type placer struct {
	JSON bool
}

func main() {
	p := &placer{}
	cmd := &cobra.Command{
		Use:           "place",
		Short:         "Geolocation information from macOS Location Services",
		RunE:          p.run,
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	cmd.Flags().BoolVarP(&p.JSON, "json", "j", false, "Print out JSON instead of human-readable text")

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		fmt.Printf("Error: %+v\n", err)
		os.Exit(1)
	}
}

func (p *placer) run(cmd *cobra.Command, args []string) error {
	loc, err := location.CurrentLocation()
	if err != nil {
		return err
	}

	if p.JSON {
		bs, err := json.MarshalIndent(&loc, "", "  ")
		if err != nil {
			return err
		}

		fmt.Println(string(bs))
		return nil
	}

	fmt.Printf("Latitude: %f\n", loc.Latitude)
	fmt.Printf("Longitude: %f\n", loc.Longitude)
	fmt.Printf("Accuracy: %f\n", loc.HorizontalAccuracy)
	if loc.VerticalAccuracy >= 0 {
		fmt.Printf("Altitude: %f (accuracy: %f)\n", loc.Altitude, loc.VerticalAccuracy)
	}

	fmt.Printf("Last observed: %s\n", loc.Timestamp.Format(time.RFC3339))

	return nil
}
