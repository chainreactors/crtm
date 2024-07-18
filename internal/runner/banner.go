package runner

import (
	"github.com/chainreactors/crtm/pkg/update"
)

const version = "v0.0.1"

//var banner = `
//                ____
//     ____  ____/ / /_____ ___
//    / __ \/ __  / __/ __ __  \
//   / /_/ / /_/ / /_/ / / / / /
//  / .___/\__,_/\__/_/ /_/ /_/
// /_/
//`
//
//// showBanner is used to show the banner to the user
//func showBanner() {
//	gologger.Print().Msgf("%s\n", banner)
//	gologger.Print().Msgf("\t\tprojectdiscovery.io\n\n")
//}

// GetUpdateCallback returns a callback function that updates crtm
func GetUpdateCallback() func() {
	return func() {
		//showBanner()
		update.GetUpdateToolCallback("crtm", version)()
	}
}
