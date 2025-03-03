owner {
    name = "John Doe"
    dob = "1990-02-01T02:34:56-08:00"
}

database {
    enabled = true
    ports = [ 8000, 8001, 8002 ]
}

servers "alpha" {
    ip = "10.0.0.1"
    role = "frontend"
}

servers "beta" {
    ip = "10.0.0.2"
    role = "backend"
}

autoscaling_rules {
    type = "cpu"
    max_threshold_percent = 50.0
}

autoscaling_rules {
    type = "memory"
    max_threshold_percent = 75.05
}
