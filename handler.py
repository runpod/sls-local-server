import runpod
import logging

def handler(job):
    logging.info(job)
    return job.get("input", {})

runpod.serverless.start({
    handler: handler
})