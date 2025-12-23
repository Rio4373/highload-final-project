import os
import random
import time
from locust import HttpUser, task, between


def metric_payload():
    return {
        "timestamp": int(time.time()),
        "cpu": round(random.uniform(5, 90), 2),
        "rps": round(random.uniform(100, 900), 2),
    }


class MetricsUser(HttpUser):
    wait_time = between(0.001, 0.01)
    host_header = os.getenv("HOST_HEADER", "streaming.local")

    def on_start(self):
        self.client.headers.update({"Host": self.host_header})

    @task(10)
    def send_metric(self):
        self.client.post("/ingest", json=metric_payload())

    @task(1)
    def read_analysis(self):
        self.client.get("/analyze")
