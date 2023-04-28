# Clace

Main repository for Clace

Clace is an open source project which natively support Sandboxing techniques.
This enables a security focussed approach to develop and deploy internal applications.
The project is implemented in Go and uses Starlark (a dialect of Python) for
application configuration. The application can call out to plugins implemented in Go.
The plugin boundary (Python to Go) allows the specification of sandboxing rules which
are enforced by the platform. As long as the application sticks to the original
rules, further application updates can be done without requiring any reauthorization. 


