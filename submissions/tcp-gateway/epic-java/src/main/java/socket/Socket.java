package socket;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.core.exc.StreamReadException;
import com.fasterxml.jackson.core.type.TypeReference;
import com.fasterxml.jackson.databind.DatabindException;
import com.fasterxml.jackson.databind.DeserializationFeature;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.cfg.CoercionAction;
import com.fasterxml.jackson.databind.cfg.CoercionInputShape;
import com.fasterxml.jackson.databind.type.LogicalType;
import jakarta.validation.ConstraintViolation;
import jakarta.validation.Validation;
import jakarta.validation.Validator;
import jakarta.validation.ValidatorFactory;
import model.Request;
import model.Response;

import java.io.*;
import java.net.InetSocketAddress;
import java.net.ServerSocket;
import java.net.SocketException;
import java.nio.ByteBuffer;
import java.util.*;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.logging.Level;
import java.util.logging.Logger;

public class Socket {
    private static final Logger log = Logger.getLogger(Socket.class.getName());

    static Map<String, Object> router = new HashMap<>();
    static Map<String, String> clients = new HashMap<>();

    static int PORT = Integer.parseInt(Optional.ofNullable(System.getenv("GATEWAY_PORT")).orElseThrow());
    static int BACKEND_TIME_OUT= Integer.parseInt(Optional.ofNullable(System.getenv("GATEWAY_BACKEND_TIMEOUT_MS")).orElseThrow());

    static int REQUEST_MIN_SIZE = 1;
    static int REQUEST_MAX_SIZE = 1048576;
    static ObjectMapper mapper = new ObjectMapper();

    public static void main(String[] args) {
        // Make this into a Config class later


        // Router file
        String routerFile = System.getenv("GATEWAY_ROUTES");


        try {
            router = mapper.readValue(new File(routerFile), new TypeReference<Map<String, Object>>() {
            });

            router.forEach((s, o) -> {
                try {
                    String[] address = o.toString().split(":");
                    if (address.length != 2) {
                        throw new Exception();
                    }

                } catch (Exception e) {
                    throw new RuntimeException(e);
                }
            });

        } catch (IOException e) {
            throw new RuntimeException(e);
        }

        // Port
        if (args.length > 0) {
            PORT = Integer.parseInt(args[0]);
        }

        // Setting up the mapper for request
        var textual = mapper.coercionConfigFor(LogicalType.Textual);
        // Fails every single type that it sees for mapper unless it is string
        for (CoercionInputShape shape : CoercionInputShape.values()) {
            // skips over string
            if (shape != CoercionInputShape.String) {
                textual.setCoercion(shape, CoercionAction.Fail);
            }
        }

        // fails on trailing data
        mapper.enable(DeserializationFeature.FAIL_ON_TRAILING_TOKENS);




        ExecutorService threadPool = Executors.newCachedThreadPool();
        try (ServerSocket serverSocket = new ServerSocket(PORT)) {
            log.info("Socket listening on port " + serverSocket.getLocalPort());
            while (true) {
                java.net.Socket client_connection = serverSocket.accept();
                threadPool.execute(() -> {
                    try {
                        _handleRequest(client_connection);
                    } catch (Error | IOException e) {
                        log.log(Level.WARNING, "Socket accept failed", e);
                    }
                });
            }



        } catch (Error | IOException e) {
            log.log(Level.WARNING, "Socket accept failed", e);
        }

    }


    static byte[] _getByteFrame(byte[] data) {
        try {
            byte[] size = ByteBuffer.allocate(4).putInt(data.length).array();
            return ByteBuffer.allocate(data.length + size.length).put(size).put(data).array();
        } catch (Error e) {
            log.log(Level.WARNING, "ByteFrame fail to create", e);
            return new byte[]{};
        }
    }

    static void _routingRequest(Request request, java.net.Socket connection, byte[] rawRequest) throws IOException {
        DataOutputStream writeDestination;
        DataInputStream readDestination;
        String[] route;
        try {
            String target = request.getTarget();
            route = router.get(target).toString().split(":");
        } catch (Error | Exception e) {
            log.log(Level.WARNING, "Invalid target", e);

            Response response = new Response();
            response.setStatus("error");
            response.setId(request.getId());
            response.setBody("backend issue" + e.getMessage());

            byte[] frame = _getByteFrame(mapper.writeValueAsBytes(response));
            connection.getOutputStream().write(frame);
            connection.getOutputStream().flush();
            return;
        }

        if (route.length != 2) {
            log.log(Level.WARNING, "Invalid route length");
            Response response = new Response();
            response.setStatus("error");
            response.setId(request.getId());
            response.setBody("backend issue");

            byte[] frame = _getByteFrame(mapper.writeValueAsBytes(response));
            connection.getOutputStream().write(frame);
            connection.getOutputStream().flush();
            return;
        }

        String host = route[0];
        String port = route[1];


        java.net.Socket backendConnection = null;
        try {
            backendConnection = new java.net.Socket(host, Integer.parseInt(port));
            backendConnection.setSoTimeout(BACKEND_TIME_OUT);

            writeDestination = new DataOutputStream(backendConnection.getOutputStream());

            byte[] requestByte = _getByteFrame(rawRequest);
            writeDestination.write(requestByte);
            writeDestination.flush();


            readDestination = new DataInputStream(backendConnection.getInputStream());

            int rawSize = readDestination.readInt();
            byte[] rawResponse = new byte[rawSize];
            readDestination.readFully(rawResponse);

            byte[] responseBytes = _getByteFrame(rawResponse);

            connection.getOutputStream().write(responseBytes);
            connection.getOutputStream().flush();

        } catch (Error | Exception e) {

            Response response = new Response();
            response.setStatus("error");
            response.setId(request.getId());
            response.setBody("backend issue" + e.getMessage());

            byte[] frame = _getByteFrame(mapper.writeValueAsBytes(response));
            connection.getOutputStream().write(frame);
            connection.getOutputStream().flush();

            if (backendConnection == null) {
                return;
            }
            backendConnection.close();

        }
    }

    static void _handleRequest(java.net.Socket client_connection) throws IOException {
        // The while loop outside only reads connections from a new client, doesn't take in account of an existing client since it is using the same tcp socket connection
        // Something I just missed
        while (client_connection.isConnected()) {

            try {
                DataInputStream stream = new DataInputStream(client_connection.getInputStream());
                int size = stream.readInt();

                if (!(REQUEST_MIN_SIZE <= size && size <= REQUEST_MAX_SIZE)) {
                    log.log(Level.WARNING, "Size not supported");
                    client_connection.close();
                }

                // Setting up the buffer for the request
                byte[] rawRequestJson = new byte[size];
                stream.readFully(rawRequestJson);

                // Creating the request object
                Request request = mapper.readValue(rawRequestJson, Request.class);
                log.log(Level.FINEST, "Request: {0}", request);
                // Keeping track of the error a bit more
                ArrayList<String> errors = new ArrayList<>();

                try (ValidatorFactory factory = Validation.buildDefaultValidatorFactory()) {
                    Validator validator = factory.getValidator();
                    Set<ConstraintViolation<Request>> violations = validator.validate(request);
                    if (!violations.isEmpty()) {
                        for (ConstraintViolation<Request> violation : violations) {
                            errors.add(violation.getMessage());
                        }
                    }
                    log.log(Level.WARNING, errors.toString());
                }


                DataOutputStream outputStream = new DataOutputStream(client_connection.getOutputStream());
                if (!errors.isEmpty()) {
                    Response response = new Response();
                    response.setBody(errors.toString());
                    response.setStatus("error");

                    byte[] responseByte = _getByteFrame(mapper.writeValueAsBytes(response));

                    // Logging errors
                    log.log(Level.WARNING, "Error on request json", errors);
                    outputStream.write(responseByte);
                    outputStream.flush();
                    client_connection.close();
                    return;
                }

                if (request.getId() != null && !clients.containsKey(request.getId())) {
                    clients.put(request.getId(), client_connection.getRemoteSocketAddress().toString());
                }


                _routingRequest(request, client_connection, rawRequestJson);


            } catch (Error | IOException e) {
                log.log(Level.WARNING, "Socket accept failed", e);
                client_connection.close();
            }
        }

    }


}
