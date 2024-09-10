
# OpenCost Plugins

OpenCost plugins make extending [OpenCost](https://github.com/opencost/opencost)’s coverage into new external cost sources (e.g. monitoring, data platforms, cloud services, and other SaaS solutions) available to the open source community. They allow for the ingestion of arbitrary cost data into OpenCost through conformance to the [FOCUS spec](https://focus.finops.org/).

# How plugins work

Any plugin released within this repository can be deployed alongside OpenCost. When deployed, OpenCost will query all installed plugins with a [request](https://github.com/opencost/opencost/blob/531641e608f404bbdc756c5dd291a44367053190/protos/customcost/messages.proto#L14-L22) for the following:
- Start - timestamp
- End - timestamp
- Resolution - either hour or day

The plugins are expected to return a [response](https://github.com/opencost/opencost/blob/531641e608f404bbdc756c5dd291a44367053190/protos/customcost/messages.proto#L28-L54) that conforms to the aforementioned FOCUS spec for the given window and resolution. OpenCost will then store the response, and continue to request data for further time ranges.

The FOCUS spec is broken up into two parts:
- [`CustomCost`](https://github.com/opencost/opencost/blob/531641e608f404bbdc756c5dd291a44367053190/protos/customcost/messages.proto#L56-L99)
- [`CustomCostExtendedAttributes`](https://github.com/opencost/opencost/blob/531641e608f404bbdc756c5dd291a44367053190/protos/customcost/messages.proto#L101-L150)
  Plugin development only necessitates the implementation of the `CustomCost` response. Extended attributes are an optional response object. However, we high encourage the use of extended attributes, as doing so will assist us in developing the UX of said attributes within OpenCost.

See [this ExcaliDraw diagram](https://app.excalidraw.com/l/ABLQ24dkKai/CBEQtjH6Mr) for a more details overview of the plugin system

# Creating a new plugin

At the most basic level, all a plugin needs to do is gather cost data given a time window and resolution. The logistics of this are straightforward, but the complexity of the implementation will depend on the data source in question.

## Plugin setup
- Pull the repo locally.
- Create a directory within the repo for the new plugin. Check out the [Datadog plugin](https://github.com/opencost/opencost-plugins/tree/main/datadog) for a reference to follow along with.
- Create the plugin subdirectories:
    - `<repo>/<plugin>/cmd/main/`
        - This will contain the actual logic of the plugin.
    - `<repo>/<plugin>/<plugin>plugin/`
        - This will contain the plugin config struct and any other structs for handling requests/responses in the plugin.
    - `<repo>/<plugin>/tests/`
        - Highly recommended, this will contain tests to validate the functionality of the plugin.
- Initialize the subproject:
    - Within `<repo>/<plugin>/`, run `go mod init github.com/opencost/opencost-plugins/<plugin>` and `go get github.com/hashicorp/go-plugin`.

## Design the configuration

All plugins require a configuration. For example, the [Datadog plugin configuration](https://github.com/opencost/opencost-plugins/blob/main/datadog/datadogplugin/datadogconfig.go) takes in some information required to authenticate with the Datadog API. This configuration will be defined by a struct inside `<repo>/<plugin>/<plugin>plugin/`.

## Implement the plugin

Once the configuration is designed, it's time to write the plugin. Within `<repo>/<plugin>/cmd/main/>`, create `main.go`:
- Create a `<plugin>Source` struct ([Datadog reference](https://github.com/opencost/opencost-plugins/blob/00809062196b79ce354a5cdafaba1d6ed3f132f9/datadog/cmd/main/main.go#L43-L47)).
- Implement the [`CustomCostSource` interface](https://github.com/opencost/opencost/blob/531641e608f404bbdc756c5dd291a44367053190/core/pkg/plugin/plugin_interface.go#L12-L14) for your plugin source ([Datadog reference](https://github.com/opencost/opencost-plugins/blob/00809062196b79ce354a5cdafaba1d6ed3f132f9/datadog/cmd/main/main.go#L49-L88)). At the time of the writing of this guide, the only required function is `GetCustomCosts`, which takes in a [`CustomCostRequest`](https://github.com/opencost/opencost/blob/develop/protos/customcost/messages.proto#L14-L22) object and returns a list of [`CustomCostResponse`](https://github.com/opencost/opencost/blob/develop/protos/customcost/messages.proto#L28-L54) objects. Let's step through the Datadog reference implementation:
    - [First](https://github.com/opencost/opencost-plugins/blob/00809062196b79ce354a5cdafaba1d6ed3f132f9/datadog/cmd/main/main.go#L52), we split the requested window into sub-windows given the requested resolution. OpenCost has a convenience function to perform this split for us ([`GetWindows`](https://github.com/opencost/opencost/blob/b9f5e42f17ae5b1b05b722dd04502bd307a6a25c/core/pkg/opencost/window.go#L1084)).
    - [Next](https://github.com/opencost/opencost-plugins/blob/00809062196b79ce354a5cdafaba1d6ed3f132f9/datadog/cmd/main/main.go#L63), we grab some pricing data from the Datadog API to prepare for the next step.
    - [Penultimately](https://github.com/opencost/opencost-plugins/blob/00809062196b79ce354a5cdafaba1d6ed3f132f9/datadog/cmd/main/main.go#L75-L85), we iterate through each sub-window, grabbing the cost data from the Datadog API for each one.
        - Optionally (but recommended), implement your response data such that the custom cost extended attributes are utilized as much as possible.
    - [Finally](https://github.com/opencost/opencost-plugins/blob/00809062196b79ce354a5cdafaba1d6ed3f132f9/datadog/cmd/main/main.go#L87), we return the retrieved cost data.
- Implement the `main` function:
    - Find the config file ([Datadog reference](https://github.com/opencost/opencost-plugins/blob/main/datadog/cmd/main/main.go#L92)).
    - Load the config file ([Datadog reference](https://github.com/opencost/opencost-plugins/blob/main/datadog/cmd/main/main.go#L97)).
    - Instantiate the plugin source ([Datadog reference](https://github.com/opencost/opencost-plugins/blob/00809062196b79ce354a5cdafaba1d6ed3f132f9/datadog/cmd/main/main.go#L104-L106)).
    - Serve the plugin for consumption by OpenCost ([Datadog reference](https://github.com/opencost/opencost-plugins/blob/00809062196b79ce354a5cdafaba1d6ed3f132f9/datadog/cmd/main/main.go#L110-L118)).

## Implement tests (highly recommended)
Write some unit tests to validate the functionality of your new plugin. See the [Datadog unit tests](https://github.com/opencost/opencost-plugins/blob/main/datadog/tests/datadog_test.go) for reference.

## Submit it!
Now that your plugin is implemented and tested, all that's left is to get it submitted for review. Create a PR based off your branch and submit it, and an OpenCost developer will review it for you.

## Plugin system limitations
- OpenCost stores the plugin responses in an in-memory repository, which necessitates that OpenCost queries the plugins again for cost data upon pod restart.
- Many cost sources have API rate limits, such as Datadog. As such, a rate limiter may be necessary.
- If you want a plugin embedded in your OpenCost image, you will have to build the image yourself.