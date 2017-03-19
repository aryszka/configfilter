package configfilter_test

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/aryszka/configfilter"
	"github.com/zalando/skipper"
	"github.com/zalando/skipper/filters"
	"github.com/zalando/skipper/routing"
)

func Example() {
	go func() {
		cf := configfilter.New(configfilter.Options{
			DefaultRoutes: configfilter.SelfRoutes,
		})
		defer cf.Close()

		if err := skipper.Run(skipper.Options{
			Address:           ":9090",
			CustomFilters:     []filters.Spec{cf},
			CustomDataClients: []routing.DataClient{cf},
		}); err != nil {
			log.Println(err)
		}
	}()

	rsp, err := http.Post("http://localhost:9090/__config", "text/plain", bytes.NewBufferString(`
		foo: Path("/foo") -> "https://foo.example.org";
		bar: Path("/bar") -> "https://bar.example.org"
	`))
	if err != nil {
		log.Println(err)
		return
	}
	defer rsp.Body.Close()

	rsp, err = http.Get("http://localhost:9090/__config")
	if err != nil {
		log.Println(err)
		return
	}
	defer rsp.Body.Close()

	io.Copy(os.Stdout, rsp.Body)

	// Output:
	// foo: Path("/foo")
	//   -> "https://foo.example.org";
	// bar: Path("/bar")
	//   -> "https://bar.example.org";
	// __config: Path("/__config")
	//   -> config()
	//   -> <shunt>;
	// __config__singleRoute: Path("/__config/:routeid")
	//   -> config()
	//   -> <shunt>
}
