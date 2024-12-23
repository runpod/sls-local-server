import runpod

def handler(job):
    return job.get("input", {})

runpod.serverless.start({
    handler: handler
})