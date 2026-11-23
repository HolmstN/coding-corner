package socket;

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
import java.util.ArrayList;
import java.util.Set;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.logging.Level;
import java.util.logging.Logger;

public class Socket{
    private int port;

    private static final Logger log = Logger.getLogger(Socket.class.getName());
    static java.net.Socket userSocket;
    static java.net.Socket orderSocket;

    static {
        try {
            userSocket = new java.net.Socket("127.0.0.1", Integer.parseInt("9001"));
            orderSocket = new java.net.Socket("127.0.0.1", Integer.parseInt("9002"));
        } catch (IOException e) {
            log.log(Level.WARNING, "Could not create socket");
            throw new RuntimeException(e);
        }
    }

    public void main(String[] args) {

        if (args.length < 1){
            throw new IllegalArgumentException();
        }

        this.port = Integer.parseInt(args[0]);
        log.info("Socket listening on port " + this.port);


        ExecutorService threadPool =  Executors.newCachedThreadPool();

        try (ServerSocket serverSocket = new ServerSocket(port)){
            while (true){
                java.net.Socket client_connection = serverSocket.accept();
                threadPool.execute(_handleRequest(client_connection));
            }

        } catch (IOException e) {
            throw new RuntimeException(e);
        }


    }

    Runnable _routingRequest(Request request, java.net.Socket connection) throws IOException {
        OutputStream writeDestination = null;
        BufferedReader readDestination = null;
        Target destination = request.getTarget();
        ObjectMapper mapper = new ObjectMapper();
        if (destination == Target.users){
            writeDestination = userSocket.getOutputStream();
            readDestination = new BufferedReader(new InputStreamReader(userSocket.getInputStream()));
        }

        if (destination == Target.orders){
            writeDestination = orderSocket.getOutputStream();

            readDestination = new BufferedReader(new InputStreamReader(orderSocket.getInputStream()));
        }

        if (writeDestination == null || readDestination == null){
            log.log(Level.WARNING, " stream not matched");
            throw new IllegalArgumentException();
        }

        writeDestination.write(mapper.writeValueAsBytes(request));
        writeDestination.flush();


        String raw_response = readDestination.readLine();
        if (raw_response == null){
            log.log(Level.WARNING, " response not matched");
        } else {
            Response response = mapper.readValue(raw_response, Response.class);

            connection.getOutputStream().write(mapper.writeValueAsBytes(response));
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
            byte[] raw_request = new byte[size];
            stream.readFully(raw_request);
            // Creating the request object
            ObjectMapper mapper = new ObjectMapper();
            Request request = mapper.readValue(raw_request, Request.class);
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

            }
            OutputStream outputStream = client_connection.getOutputStream();

            if (!errors.isEmpty()){
                Response response = new Response();
                response.setId(request.getId() != null ?  request.getId() : "No ID");
                response.setBody(errors.toString());
                response.setStatus("error");
                outputStream.write(mapper.writeValueAsBytes(response));

                // Logging errors
                log.log(Level.WARNING, "Error on request json", errors);

                outputStream.flush();
                client_connection.close();
            }

            _routingRequest(request, client_connection);




        } catch (IOException e) {
            throw new RuntimeException(e);
        }
        return null;
    }


}
