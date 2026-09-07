# seabird-nwwsio-plugin

The [NOAA Weather Wire Service](https://www.weather.gov/nwws/) (NWWS) is the
National Weather Service's channel for distributing everything its forecast
offices and national centers issue: warnings, watches, advisories, forecasts,
observations and administrative messages, usually within seconds of issuance.
Its Open Interface (NWWS-OI) is an XMPP chat room that receives every product
as it is transmitted.

This plugin joins that room on behalf of a [seabird](https://github.com/seabird-chat)
bot. Each product is parsed for its issuing office, product type and covered
area, and the ones people subscribe to are delivered to them as private messages.
Subscriptions can be by issuing office, by SAME county code, or by ZIP code,
and each subscription can be narrowed by product category.

## Commands

Everything is driven by the `!noaa` command. `!noaa help` prints:

```
NOAA Weather Alerts commands:
!noaa subscribe station <CODE> [filters...]  e.g. !noaa subscribe station KJAX warning,watch
!noaa subscribe same <CODE> [<CODE>...] [filters...]  e.g. !noaa subscribe same 012031 012109
!noaa subscribe zip <ZIP> [<ZIP>...] [filters...]  e.g. !noaa subscribe zip 48103-1680 all
!noaa unsubscribe station <CODE> | same <CODE...>|all | zip <ZIP...>|all | all
!noaa list | recent <CODE> | filters | help
Filters default to cap (emergency alerts only). Use '!noaa filters' for the full list.
```

| Command | What it does |
|---------|--------------|
| `subscribe station <CODE>` | Everything a forecast office issues, by its four-letter ID (e.g. `KJAX`). |
| `subscribe same <CODE>...` | Products covering one or more six-digit SAME county codes. |
| `subscribe zip <ZIP>...` | Same as above, with ZIP codes mapped to their counties. |
| `unsubscribe ...` | Drop one subscription, all of one kind, or all of them. |
| `list` | Show your subscriptions and their filters. |
| `recent <CODE>` | The last few products seen from an office. |
| `filters` | List the filter names below. |

Filters follow the codes, separated by spaces or commas. `!noaa filters` prints:

```
Valid filter options:
Special: all, cap
Categories: Administrative, Advisory, Agriculture, Alert, Avalanche, Aviation, Climate, Data, Discussion, Emergency, Fire Weather, Forecast, Health, Hydrology, Marine, Observation, Outlook, Public Info, Recon, Request, Satellite, Space Weather, Statement, Statistics, Summary, Test, Travel, Tropical, Verification, Warning, Watch
```

## Finding your SAME code

SAME (Specific Area Message Encoding) codes are the six-digit county
identifiers used by NOAA Weather Radio and the Emergency Alert System. Pick
your state on the NWS [County Coverage By State](https://www.weather.gov/nwr/counties)
page to find the code for your county, or search the complete
[SameCode.txt](https://www.weather.gov/source/nwr/SameCode.txt) listing.

## CAP and the default filter

CAP is the Common Alerting Protocol, the structured XML form of an alert. The
NWS publishes a CAP version of every hazard product (warnings, watches,
advisories and statements) alongside the plain-text product, carrying the event
name, severity, urgency, certainty, headline, description, instructions and the
SAME codes of the affected counties.

The default filter, `cap`, delivers only these CAP alerts, so a subscriber
hears about hazards and nothing else. `all` delivers every product, and a
category name delivers the plain-text products of that category. NWS test
traffic (the hourly test messages, AWIPS communications tests, and CAP alerts
with a Test or Exercise status) is dropped before delivery unless the plugin is
started with `FILTER_TEST_MESSAGES=false`.
