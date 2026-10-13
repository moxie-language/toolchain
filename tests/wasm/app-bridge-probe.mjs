// The application half of web_app_bridge.mx.
//
// The host is framework; a bridge is the app's. This module's default export is
// merged over the stdlib bridge in the Worker that runs the bundle, which is how
// an app answers imports the framework knows nothing about.
export default {
  app_bridge_probe: (n) => n + 1,
};
