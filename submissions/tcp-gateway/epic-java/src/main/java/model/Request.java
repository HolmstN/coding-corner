package model;

import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotNull;
import lombok.Data;



@Data
public class Request {

    @NotBlank
    private String id;
    @NotBlank
    private String target;
    @NotNull
    private String body;
}