## Uber Pane
Ninja Sphere - Application that adds an Uber pane to the LED Matrix

To use you'll need a config.json with the app secrets.

You probably want to run it from your mac like this:

```go build && DEBUG=* ./app-uber --led.host elliotsphere2.local --mqtt.host elliotsphere2.local```

You can specify a latitude/longitude with ```--latitude=-33.86 --longitude=-151.20```
