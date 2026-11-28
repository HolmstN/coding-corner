package model;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotNull;
import jakarta.validation.constraints.Pattern;
import lombok.Data;



@Data
// JsonIgnoreProperties doesn't keep the fields, just stop the throw
@JsonIgnoreProperties(ignoreUnknown = true)
public class Request {

    @NotBlank
//    @Pattern(regexp = "[0-9].*")
    private String id;
    @NotBlank
    private String target;
    @NotNull
    private String body;
}