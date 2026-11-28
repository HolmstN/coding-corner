package model;

import lombok.Data;

@Data
public class Route {
    String users;
    String orders;
    String boundary;

    public String getUserHost() {
        return users.split(":")[0];
    }

    public String getUserPort() {
        return users.split(":")[1];
    }

    public String getOrderHost() {
        return orders.split(":")[0];
    }


    public String getOrderPort() {
        return orders.split(":")[1];
    }
}
