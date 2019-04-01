## SSH-CONNS

Register and monitor ssh connections in your server.

## Objectives

- Have visibility of all ssh connections
- Extractable logs of ssh connection history


# How

ssh conns listens to connections established via ssh. It registers each connection unique identidied with
its PID. It the monitors every connection and upates it log entry once the connection is lost.

# How to extend

This package can be extended to ;

 - Extract logs to a csv and sent via email periodically
 - Send email alerts whenever a new connection is made
 - Connections need not only be ssh. All connections can be monitored if need be
 