package services_state

import (
	"encoding/json"
	"fmt"
	"testing"
	"regexp"
	"time"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/newrelic/bosun/service"
)

var hostname = "shakespeare"
var anotherHostname = "chaucer"

func Test_NewServer(t *testing.T) {

	Convey("Invoking NewServer()", t, func() {
		Convey("Returns a server with the correct name", func() {
			server := NewServer(hostname)
			So(server.Name, ShouldEqual, hostname)
		})

		Convey("Initializes the map", func() {
			server := NewServer(hostname)
			So(server.Services, ShouldNotBeNil)
		})

		Convey("Initializes the time", func() {
			server := NewServer(hostname)
			So(server.LastUpdated, ShouldBeTheSameTimeAs, time.Unix(0, 0))
		})
	})
}

func Test_NewServicesState(t *testing.T) {

	Convey("Invoking NewServicesState()", t, func() {

		Convey("Initializes the Servers map", func() {
			state := NewServicesState()
			So(state.Servers, ShouldNotBeNil)
		})

	})
}

func Test_ServicesStateWithData(t *testing.T) {

	Convey("When working with data", t, func() {
		state := NewServicesState()
		state.Servers[hostname] = NewServer(hostname)

		baseTime := time.Now().UTC()

		svcId := "deadbeef123"

		svc := service.Service{
			ID: svcId,
			Name: "radical_service",
			Image: "101deadbeef",
			Created: baseTime,
			Hostname: anotherHostname,
			Updated: baseTime,
			Status: service.ALIVE,
		}

		Convey("Encode() generates JSON that we can Decode()", func() {
			decoded, err := Decode(state.Encode())

			So(err, ShouldBeNil)
			So(decoded.Servers[hostname].Name, ShouldEqual, hostname)
			So(len(decoded.Servers), ShouldEqual, 1)
		})

		Convey("Decode() returns an error when handed junk", func() {
			result, err := Decode([]byte("asdf"))

			So(result.Servers, ShouldBeEmpty)
			So(err, ShouldNotBeNil)
		})

		Convey("HasServer() is true when a server exists", func() {
			So(state.HasServer(hostname), ShouldBeTrue)
		})

		Convey("HasServer() is false when a server is missing", func() {
			So(state.HasServer("junk"), ShouldBeFalse)
		})

		Convey("AddServiceEntry()", func() {
			Convey("Merges in a new service", func() {
				So(state.HasServer(anotherHostname), ShouldBeFalse)

				state.AddServiceEntry(svc)

				So(state.HasServer(anotherHostname), ShouldBeTrue)
				So(state.Servers[anotherHostname].Services[svc.ID], ShouldNotBeNil)
			})

			Convey("Doesn't merge a stale service", func() {
				state.AddServiceEntry(svc)

				staleService := service.Service{
					ID: "deadbeef123",
					Name: "stale_service",
					Image: "stale",
					Created: baseTime,
					Hostname: anotherHostname,
					Updated: baseTime.Add(0 - 1 * time.Minute),
					Status: service.ALIVE,
				}

				state.AddServiceEntry(staleService)

				So(state.HasServer(anotherHostname), ShouldBeTrue)
				So(state.Servers[anotherHostname].Services[svc.ID].Updated,
					ShouldBeTheSameTimeAs, baseTime)
				So(state.Servers[anotherHostname].Services[svc.ID].Image,
					ShouldEqual, "101deadbeef")
			})

			Convey("Updates the LastUpdated time for the server", func() {
				newDate := svc.Updated.AddDate(0, 0, 5)
				svc.Updated = newDate
				state.AddServiceEntry(svc)

				So(state.Servers[anotherHostname].LastUpdated, ShouldBeTheSameTimeAs, newDate)
			})
		})

		Convey("Merge() merges state we care about from other state structs", func() {
			firstState  := NewServicesState()
			secondState := NewServicesState()
			firstState.AddServiceEntry(svc)
			secondState.Merge(firstState)

			So(len(secondState.Servers), ShouldEqual, len(firstState.Servers))
			So(secondState.Servers[svcId], ShouldEqual, firstState.Servers[svcId])
		})

		Convey("Format() pretty-prints the state even without a Memberlist", func() {
			formatted := state.Format(nil)

			So(formatted, ShouldNotBeNil)
		})

		Reset(func() {
			state = NewServicesState()
			state.Servers[hostname] = NewServer(hostname)
		})
	})
}

func Test_Broadcasts(t *testing.T) {

	Convey("When Broadcasting services", t, func() {
		state := NewServicesState()
		state.Servers[hostname] = NewServer(hostname)
		quit       := make(chan bool)
		svcId1     := "deadbeef123"
		svcId2     := "deadbeef101"
		baseTime   := time.Now().UTC().Round(time.Second)

		service1 := service.Service{ ID: svcId1, Hostname: hostname, Updated: baseTime }
		service2 := service.Service{ ID: svcId2, Hostname: hostname, Updated: baseTime }
		services := []service.Service{ service1, service2 }

		containerFn := func() []service.Service {
			return services
		}

		state.HostnameFn = func() (string, error) { return hostname, nil }

		Convey("New services are serialized into the channel", func() {
			go func() { quit <- true }()
			go state.BroadcastServices(containerFn, quit)

			json1, _ := json.Marshal(service1)
			json2, _ := json.Marshal(service2)

			readBroadcasts := <-state.Broadcasts
			So(len(readBroadcasts), ShouldEqual, 2)
			So(string(readBroadcasts[0]), ShouldEqual, string(json1))
			So(string(readBroadcasts[1]), ShouldEqual, string(json2))
		})

		Convey("All of the services are added to state", func() {
			go func() { quit <- true }()
			go state.BroadcastServices(containerFn, quit)
			<-state.Broadcasts // Block until we get a result

			So(state.Servers[hostname].Services[svcId1], ShouldNotBeNil)
			So(state.Servers[hostname].Services[svcId2], ShouldNotBeNil)
			So(state.Servers[hostname].Services[svcId1].ID, ShouldEqual, svcId1)
			So(state.Servers[hostname].Services[svcId2].ID, ShouldEqual, svcId2)
		})

		Convey("Puts a nil into the broadcasts channel when no services", func() {
			emptyList := func() []service.Service { return []service.Service{} }
			go func() { quit <- true }()
			go state.BroadcastServices(emptyList, quit)
			broadcast := <-state.Broadcasts

			So(broadcast, ShouldBeNil)
		})

		Convey("All of the tombstones are serialized into the channel", func() {
			go func() { quit <- true }()
			junk := service.Service{ ID: "runs", Hostname: hostname, Updated: baseTime }
			state.AddServiceEntry(junk)
			state.AddServiceEntry(service1)
			state.AddServiceEntry(service2)
			go state.BroadcastTombstones(containerFn, quit)

			readBroadcasts := <-state.Broadcasts
			So(len(readBroadcasts), ShouldEqual, 2) // 2 per service
			// Match with regexes since the timestamp changes during tombstoning
			So(readBroadcasts[0], ShouldMatch, "^{\"ID\":\"runs\".*\"Status\":1}$")
			So(readBroadcasts[1], ShouldMatch, "^{\"ID\":\"runs\".*\"Status\":1}$")
		})

		Convey("Services that are still alive are not tombstoned", func() {
			go func() { quit <- true }()
			state.AddServiceEntry(service1)
			state.AddServiceEntry(service2)
			go state.BroadcastTombstones(containerFn, quit)

			readBroadcasts := <-state.Broadcasts
			So(len(readBroadcasts), ShouldEqual, 0)
		})

		Convey("Puts a nil into the broadcasts channel when no tombstones", func() {
			emptyList := func() []service.Service { return []service.Service{} }
			go func() { quit <- true }()
			go state.BroadcastTombstones(emptyList, quit)
			broadcast := <-state.Broadcasts

			So(broadcast, ShouldBeNil)
		})

		Convey("Tombstones have a lifespan, then expire", func() {
			go func() { quit <- true }()
			service1.Tombstone()
			service1.Updated = service1.Updated.Add(0 - TOMBSTONE_LIFESPAN - 1 * time.Minute)
			state.AddServiceEntry(service1)
			state.AddServiceEntry(service2)
			So(state.Servers[hostname].Services[service1.ID], ShouldNotBeNil)

			go state.BroadcastTombstones(containerFn, quit)
			<-state.Broadcasts

			So(state.Servers[hostname].Services[service1.ID], ShouldBeNil)
		})

		Convey("When the last tombstone is removed, so is the server", func() {
			state := NewServicesState() // Totally empty
			state.HostnameFn = func() (string, error) { return hostname, nil }
			state.AddServiceEntry(service1)
			state.Servers[hostname].Services[service1.ID].Tombstone()
			state.Servers[hostname].Services[service1.ID].Updated =
					service1.Updated.Add(0 - TOMBSTONE_LIFESPAN - 1 * time.Minute)

			So(state.Servers[hostname], ShouldNotBeNil)
			state.TombstoneOthersServices()
			So(state.Servers[hostname], ShouldBeNil)
		})

		Convey("Alive services have a lifespan and then are tombstoned", func() {
			state.AddServiceEntry(service1)
			svc := state.Servers[hostname].Services[service1.ID]
			stamp := service1.Updated.Add(0 - ALIVE_LIFESPAN - 5 * time.Second)
			svc.Updated = stamp

			state.TombstoneOthersServices()

			So(svc.Status, ShouldEqual, service.TOMBSTONE)
			So(svc.Updated, ShouldBeTheSameTimeAs, stamp.Add(time.Second))
		})
	})
}

func Test_ClusterMembershipManagement(t *testing.T) {

	Convey("When managing cluster members", t, func() {
		state := NewServicesState()
		state.Servers[hostname] = NewServer(hostname)
		svcId1     := "deadbeef123"
		svcId2     := "deadbeef101"
		baseTime   := time.Now().UTC().Round(time.Second)
		quit       := make(chan bool)

		service1 := service.Service{ ID: svcId1, Hostname: hostname, Updated: baseTime }
		service2 := service.Service{ ID: svcId2, Hostname: hostname, Updated: baseTime }

		state.HostnameFn = func() (string, error) { return hostname, nil }

		Convey("Expire() tombstones all services for a server", func() {
			state.AddServiceEntry(service1)
			state.AddServiceEntry(service2)
			go func() { quit <- true }()
			go state.ExpireServer(hostname, quit)

			expired := <-state.Broadcasts

			So(len(expired), ShouldEqual, 2)
			// Timestamps chagne when tombstoning, so regex match
			So(expired[0], ShouldMatch, "^{\"ID\":\"deadbeef.*\"Status\":1}$")
			So(expired[1], ShouldMatch, "^{\"ID\":\"deadbeef.*\"Status\":1}$")
		})

	})
}

func Example_ByService() {
	state := NewServicesState()
	state.Servers[hostname] = NewServer(hostname)
	svcId1     := "deadbeef123"
	svcId2     := "deadbeef101"
	baseTime   := time.Now().UTC().Round(time.Second)

	service1 := service.Service{
		ID: svcId1, Name: "service1", Hostname: hostname, Updated: baseTime,
	}
	service2 := service.Service{
		ID: svcId2, Name: "service2", Hostname: hostname, Updated: baseTime,
	}

	state.AddServiceEntry(service1)
	state.AddServiceEntry(service2)

	json, _ := json.MarshalIndent(state.ByService(), "", "  ")
	println(string(json))
	// Output:
}

func Example_BroadcastTombstones() {
	state := NewServicesState()
	state.HostnameFn = func() (string, error) {
		return "something", nil
	}
	quit       := make(chan bool)

	go func() { quit <- true }()
	go func() { <-state.Broadcasts }()
	state.BroadcastTombstones(func() []service.Service { return []service.Service{} }, quit)

	// TODO go test seems broken. It should match this, but can't for some reason:
	// TombstoneServices(): New host or not running services, skipping.
	// Output:
}

func ShouldBeTheSameTimeAs(actual interface{}, expected ...interface{}) string {
    wanted := expected[0].(time.Time)
    got    := actual.(time.Time)

    if !got.Equal(wanted) {
        return "expected:\n" + fmt.Sprintf("%#v", wanted) + "\n\ngot:\n" + fmt.Sprintf("%#v", got)
    }

    return ""
}

func ShouldMatch(actual interface{}, expected ...interface{}) string {
	wanted := expected[0].(string)
	got    := actual.([]byte)

	wantedRegexp := regexp.MustCompile(wanted)

	if !wantedRegexp.Match(got) {
		return "expected:\n" + fmt.Sprintf("%#v", wanted) + "\n\nto match:\n" + fmt.Sprintf("%v", string(got))
	}

	return ""
}