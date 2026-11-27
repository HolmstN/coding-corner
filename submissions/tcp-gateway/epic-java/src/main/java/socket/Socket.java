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
import model.Target;

import java.io.*;
import java.net.ServerSocket;
import java.nio.ByteBuffer;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.Map;
import java.util.Set;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.logging.Level;
import java.util.logging.Logger;

public class Socket{
    private static final Logger log = Logger.getLogger(Socket.class.getName());

    static Map<String, Object> router = new HashMap<>();
    Map<String, String> clients =  new HashMap<>();


    public static void main(String[] args)  {
        int port;

        String routerFile = System.getenv("GATEWAY_ROUTES");

        ObjectMapper mapper = new ObjectMapper();
        try {
            router = mapper.readValue(new File(routerFile), new TypeReference<Map<String, Object>>(){});
        } catch (IOException e) {
            throw new RuntimeException(e);
        }


        port = Integer.parseInt(args[0]);

        Socket gateway = new Socket();

        ExecutorService threadPool =  Executors.newCachedThreadPool();

        try (ServerSocket serverSocket = new ServerSocket(port)){
            log.info("Socket listening on port " + serverSocket.getLocalPort());
            while (true){
                java.net.Socket client_connection = serverSocket.accept();
                threadPool.execute(() -> gateway._handleRequest(client_connection));
            }

        } catch (IOException e) {
            throw new RuntimeException(e);
        }


    }

    byte[] _getByteFrame(Object data) throws JsonProcessingException {
        ObjectMapper mapper = new ObjectMapper();
        try {
            byte[] requestBytes = mapper.writeValueAsBytes(data);
            // Hard coding length header frame size
            byte[] size = ByteBuffer.allocate(4).putInt(requestBytes.length).array();
            return ByteBuffer.allocate(requestBytes.length + size.length).put(size).put(requestBytes).array();
        } catch (Error e) {
            throw new RuntimeException(e);
        }
    }

    Runnable _routingRequest(Request request, java.net.Socket connection) throws IOException {

            DataOutputStream writeDestination = null;
            DataInputStream readDestination = null;

            String target = request.getTarget();

            String[] route = router.get(target).toString().split(":");

            String host = route[0];
            String port = route[1];

            java.net.Socket backendConnection = new java.net.Socket(host, Integer.parseInt(port));
            backendConnection.setSoTimeout(3000);

        try {
            writeDestination = new DataOutputStream(backendConnection.getOutputStream());

            byte[] requestByte = _getByteFrame(request);
            writeDestination.write(requestByte);
            writeDestination.flush();


            readDestination = new DataInputStream(backendConnection.getInputStream());

            int rawSize= readDestination.readInt();
            byte[] rawResponse = new byte[rawSize];
            readDestination.readFully(rawResponse);
            // Fix this later
            if (rawResponse == null){
                log.log(Level.WARNING, " response not matched");
            } else {
                ObjectMapper mapper = new ObjectMapper();
                Response response = mapper.readValue(rawResponse, Response.class);
                byte[] responseBytes = _getByteFrame(response);

                connection.getOutputStream().write(responseBytes);
                connection.getOutputStream().flush();
                connection.close();
            }
        } catch (IOException e) {
            backendConnection.close();

            Response  response = new Response();
            response.setStatus("error");
            response.setId(request.getId());

            byte[] frame = _getByteFrame(response);
            connection.getOutputStream().write(frame);

            connection.close();

        }

        return null;
    };

    Runnable _handleRequest(java.net.Socket client_connection){
        try {

            DataInputStream stream= new DataInputStream(client_connection.getInputStream());
            int size = stream.readInt();

            if (!(1 <= size && size <= 1048576)){
                log.log(Level.WARNING, " size not matched");
                client_connection.close();
            }

            byte[] rawRequestJson = new byte[size];
            stream.readFully(rawRequestJson);

            // Creating the request object
            ObjectMapper mapper = new ObjectMapper();
            Request request = mapper.readValue(rawRequestJson, Request.class);
            // Keeping track of the error a bit more
            ArrayList<String> errors = new ArrayList<>();

            try (ValidatorFactory factory = Validation.buildDefaultValidatorFactory()){
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
            if (!errors.isEmpty()){
                Response response = new Response();
                response.setId(request.getId() != null ?  request.getId() : "No ID");
                response.setBody(errors.toString());
                response.setStatus("error");

                byte[] responseByte = _getByteFrame(response);

                // Logging errors
                log.log(Level.WARNING, "Error on request json", errors);
                outputStream.write(responseByte);
                outputStream.flush();
                client_connection.close();
            }

            if (request.getId() != null && !clients.containsKey(request.getId())){
                clients.put(request.getId(), client_connection.getRemoteSocketAddress().toString());
            }


            _routingRequest(request, client_connection);




        } catch (IOException e) {
            throw new RuntimeException(e);
        }
        return null;
    }


}
