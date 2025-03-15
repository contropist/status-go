import random
import pytest
from steps.status_backend import StatusBackendSteps


class TestRpc(StatusBackendSteps):

    @pytest.mark.parametrize(
        "method, params",
        [
            ("wakuext_peers", []),
        ],
    )
    def test_valid_rpc_requests(self, method, params):
        _id = str(random.randint(1, 8888))

        response = self.rpc_client.rpc_valid_request(method, params, _id)
        self.rpc_client.verify_json_schema(response.json(), method)
