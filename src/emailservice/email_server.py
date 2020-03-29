#!/usr/bin/python
#
# Copyright 2018 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

from concurrent import futures
import argparse
import os
import sys
import time
import grpc
from jinja2 import Environment, FileSystemLoader, select_autoescape, TemplateError
from google.api_core.exceptions import GoogleAPICallError

import demo_pb2
import demo_pb2_grpc
from grpc_health.v1 import health_pb2
from grpc_health.v1 import health_pb2_grpc

from jaeger_client import Config

from grpc_opentracing import open_tracing_server_interceptor
from grpc_opentracing.grpcext import intercept_server


from logger import getJSONLogger
logger = getJSONLogger('emailservice-server')

# Loads confirmation email template from file
env = Environment(
    loader=FileSystemLoader('templates'),
    autoescape=select_autoescape(['html', 'xml'])
)
template = env.get_template('confirmation.html')


class DummyEmailService():
    def SendOrderConfirmation(self, request, context):
        logger.info('A request to send order confirmation email to {} has been received.'.format(
            request.email))
        return demo_pb2.Empty()


class HealthCheck():
    def Check(self, request, context):
        return health_pb2.HealthCheckResponse(
            status=health_pb2.HealthCheckResponse.SERVING)


def start(dummy_mode):
    # jaeger config
    config = Config(
        config={
            'sampler': {
                'type': 'const',
                'param': 1,
            },
            'local_agent': {
                'reporting_host': os.environ.get('JAEGER_AGENT_HOST'),
                'reporting_port': os.environ.get('JAEGER_AGENT_PORT'),
            },
            'logging': True,
        },
        service_name='emailservice'
    )

    # intialize tracer
    tracer = config.initialize_tracer()
    tracer_interceptor = open_tracing_server_interceptor(tracer)

    server = grpc.server(futures.ThreadPoolExecutor(max_workers=10))
    server = intercept_server(server, tracer_interceptor)

    service = None
    if dummy_mode:
        service = DummyEmailService()
    else:
        raise Exception('non-dummy mode not implemented yet')

    demo_pb2_grpc.add_EmailServiceServicer_to_server(service, server)
    health_pb2_grpc.add_HealthServicer_to_server(service, server)

    port = os.environ.get('PORT', "8080")
    logger.info("listening on port: "+port)
    server.add_insecure_port('[::]:'+port)
    server.start()
    try:
        while True:
            time.sleep(3600)
    except KeyboardInterrupt:
        server.stop(0)


if __name__ == '__main__':
    logger.info('starting the email service in dummy mode.')
    start(dummy_mode=True)
