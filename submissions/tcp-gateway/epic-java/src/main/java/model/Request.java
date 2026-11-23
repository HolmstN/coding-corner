package model;

import jakarta.validation.constraints.NotBlank;
import lombok.Data;



@Data
public class Request {

    @NotBlank
    private String id;
    @NotBlank
    private Target target;
    private String body;
}