package socket;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.core.exc.StreamReadException;
import com.fasterxml.jackson.core.type.TypeReference;
import com.fasterxml.jackson.databind.DatabindException;
import com.fasterxml.jackson.databind.ObjectMapper;
import jakarta.validation.ConstraintViolation;
import jakarta.validation.Validation;
import jakarta.validation.Validator;
import jakarta.validation.ValidatorFactory;
import model.Request;
import model.Response;

import java.io.*;
import java.net.ServerSocket;
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

    static int PORT = Integer.parseInt(Optional.ofNullable(System.getenv("GATEWAY_PORT")).orElse("7000"));
    static int BACKEND_TIME_OUT= Integer.parseInt(Optional.ofNullable(System.getenv("GATEWAY_BACKEND_TIMEOUT_MS")).orElse("2000"));

    static int REQUEST_MIN_SIZE = 1;
    static int REQUEST_MAX_SIZE = 1048576;

    public static void main(String[] args) {
        // Make this into a Config class later


        // Router file
        String routerFile = System.getenv("GATEWAY_ROUTES");

        ObjectMapper mapper = new ObjectMapper();
        try {
            router = mapper.readValue(new File(routerFile), new TypeReference<Map<String, Object>>() {
            });
        } catch (IOException e) {
            throw new RuntimeException(e);
        }


        if (args.length > 0) {
            PORT = Integer.parseInt(args[0]);
        }

        ExecutorService threadPool = Executors.newCachedThreadPool();
        try (ServerSocket serverSocket = new ServerSocket(PORT)) {
            log.info("Socket listening on port " + serverSocket.getLocalPort());
            while (true) {
                java.net.Socket client_connection = serverSocket.accept();
                threadPool.execute(() -> {
                    try {
                        _handleRequest(client_connection);
                    } catch (Error e) {
                        log.log(Level.WARNING, "Socket accept failed", e);
                        try {
                            client_connection.close();
                        } catch (IOException ex) {
                            log.log(Level.WARNING, "Socket close failed", ex);
                        }
                    }
                });
            }



        } catch (Error | IOException e) {
            log.log(Level.WARNING, "Socket accept failed", e);
        }

    }


    static byte[] _getByteFrame(Object data) {
        ObjectMapper mapper = new ObjectMapper();
        try {
            byte[] requestBytes = mapper.writeValueAsBytes(data);
            // Hard coding length header frame size
            byte[] size = ByteBuffer.allocate(4).putInt(requestBytes.length).array();
            return ByteBuffer.allocate(requestBytes.length + size.length).put(size).put(requestBytes).array();
        } catch (Error | JsonProcessingException e) {
            log.log(Level.WARNING, "ByteFrame fail to create", e);
            return new byte[]{};
        }
    }

    static void _routingRequest(Request request, java.net.Socket connection) throws IOException {
        DataOutputStream writeDestination;
        DataInputStream readDestination;

        String target = request.getTarget();

        String[] route = router.get(target).toString().split(":");

        String host = route[0];
        String port = route[1];

        java.net.Socket backendConnection = new java.net.Socket(host, Integer.parseInt(port));
        try {
            backendConnection.setSoTimeout(BACKEND_TIME_OUT);

            writeDestination = new DataOutputStream(backendConnection.getOutputStream());

            byte[] requestByte = _getByteFrame(request);
            writeDestination.write(requestByte);
            writeDestination.flush();


            readDestination = new DataInputStream(backendConnection.getInputStream());

            int rawSize = readDestination.readInt();
            byte[] rawResponse = new byte[rawSize];
            readDestination.readFully(rawResponse);

            ObjectMapper mapper = new ObjectMapper();
            Response response = mapper.readValue(rawResponse, Response.class);
            byte[] responseBytes = _getByteFrame(response);

            connection.getOutputStream().write(responseBytes);
            connection.getOutputStream().flush();

        } catch (Error e) {
            backendConnection.close();

            Response response = new Response();
            response.setStatus("error");
            response.setId(request.getId());

            byte[] frame = _getByteFrame(response);
            connection.getOutputStream().write(frame);

            connection.close();
        }
    }

    static void _handleRequest(java.net.Socket client_connection) {
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
                ObjectMapper mapper = new ObjectMapper();
                Request request = mapper.readValue(rawRequestJson, Request.class);
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

                    byte[] responseByte = _getByteFrame(response);

                    // Logging errors
                    log.log(Level.WARNING, "Error on request json", errors);
                    outputStream.write(responseByte);
                    outputStream.flush();
                    client_connection.close();
                }

                if (request.getId() != null && !clients.containsKey(request.getId())) {
                    clients.put(request.getId(), client_connection.getRemoteSocketAddress().toString());
                }


                _routingRequest(request, client_connection);


            } catch (Error | IOException e) {
                log.log(Level.WARNING, "Socket accept failed", e);
            }
        }
    }


}
