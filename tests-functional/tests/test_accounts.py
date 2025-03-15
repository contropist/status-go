import random

import pytest

from resources.constants import user_1
from steps.status_backend import StatusBackendSteps


@pytest.mark.accounts
@pytest.mark.rpc
class TestAccounts(StatusBackendSteps):

    @pytest.mark.parametrize(
        "method, params",
        [
            ("accounts_getAccounts", []),
            ("accounts_getKeypairs", []),
            # ("accounts_hasPairedDevices", []), # randomly crashes app, to be reworked/fixed
            # ("accounts_remainingAccountCapacity", []), # randomly crashes app, to be reworked/fixed
            ("multiaccounts_getIdentityImages", [user_1.private_key]),
        ],
    )
    def test_(self, method, params):
        _id = str(random.randint(1, 8888))

        response = self.rpc_client.rpc_valid_request(method, params, _id)
        self.rpc_client.verify_json_schema(response.json(), method)
