import json
import random


def handler(event, context):
    print("Received event: " + json.dumps(event, indent=2))

    a = random.randint(0, 28)
    if a == 0:
        raise RuntimeError("Test dead letter queue error")
