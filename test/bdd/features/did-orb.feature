#
# Copyright SecureKey Technologies Inc. All Rights Reserved.
#
# SPDX-License-Identifier: Apache-2.0
#

@did-orb
Feature:
  Background: Setup
    Given host-meta document is uploaded to IPNS
    Given variable "domain1IRI" is assigned the value "https://orb.domain1.com/services/orb"
    And variable "domain2IRI" is assigned the value "https://orb.domain2.com/services/orb"
    And variable "domain3IRI" is assigned the value "https://orb.domain3.com/services/orb"
    And variable "domain4IRI" is assigned the value "https://orb.domain4.com/services/orb"

    Given variable "domain1ID" is assigned the value "${domain1IRI}"
    And variable "domain2ID" is assigned the value "did:web:orb.domain2.com:services:orb"
    And variable "domain3ID" is assigned the value "${domain3IRI}"
    And variable "domain4ID" is assigned the value "${domain4IRI}"

    Given host "orb.domain1.com" is mapped to "localhost:48326"
    And host "orb2.domain1.com" is mapped to "localhost:48526"
    And host "orb.domain2.com" is mapped to "localhost:48426"
    And host "orb.domain3.com" is mapped to "localhost:48626"
    And host "orb.domain4.com" is mapped to "localhost:48726"
    And host "orb.domain5.com" is mapped to "localhost:49026"

    Given anchor origin for host "orb.domain1.com" is set to "https://orb.domain1.com"
    And anchor origin for host "orb2.domain1.com" is set to "ipns://k51qzi5uqu5dgkmm1afrkmex5mzpu5r774jstpxjmro6mdsaullur27nfxle1q"
    And anchor origin for host "orb.domain2.com" is set to "did:web:orb.domain2.com:services:orb"
    And anchor origin for host "orb.domain3.com" is set to "https://orb.domain3.com"
    And anchor origin for host "orb.domain4.com" is set to "https://orb.domain1.com"
    And anchor origin for host "orb.domain5.com" is set to "did:web:orb.domain5.com:services:anchor"

    Given the authorization bearer token for "POST" requests to path "/services/orb/outbox" is set to "ADMIN_TOKEN"
    And the authorization bearer token for "POST" requests to path "/services/orb/acceptlist" is set to "ADMIN_TOKEN"
    And the authorization bearer token for "GET" requests to path "/services/orb" is set to "READ_TOKEN"
    And the authorization bearer token for "GET" requests to path "/sidetree/v1/identifiers" is set to "READ_TOKEN"
    And the authorization bearer token for "POST" requests to path "/sidetree/v1/operations" is set to "ADMIN_TOKEN"
    And the authorization bearer token for "POST" requests to path "/policy" is set to "ADMIN_TOKEN"
    And the authorization bearer token for "GET" requests to path "/cas" is set to "READ_TOKEN"
    And the authorization bearer token for "GET" requests to path "/vc" is set to "READ_TOKEN"
    And the authorization bearer token for "POST" requests to path "/log" is set to "ADMIN_TOKEN"
    And the authorization bearer token for "GET" requests to path "/allowedorigins" is set to "READ_TOKEN"
    And the authorization bearer token for "POST" requests to path "/allowedorigins" is set to "ADMIN_TOKEN"

    # set up logs for domains
    When an HTTP POST is sent to "https://orb.domain1.com/log" with content "http://orb.vct:8077/maple2020" of type "text/plain"
    When an HTTP POST is sent to "https://orb.domain3.com/log" with content "http://orb.vct:8077/maple2020" of type "text/plain"

    Then we wait 1 seconds

    When an HTTP GET is sent to "https://orb.domain1.com/log"
    Then the response equals "http://orb.vct:8077/maple2020"
    When an HTTP GET is sent to "https://orb.domain2.com/log" and the returned status code is 404

    # domain1 adds domain2 and domain3 to its 'follow' and 'invite-witness' accept lists.
    Given variable "domain1AcceptList" is assigned the JSON value '[{"type":"follow","add":["${domain2ID}","${domain3ID}"]},{"type":"invite-witness","add":["${domain2ID}","${domain3ID}"]}]'
    When an HTTP POST is sent to "${domain1IRI}/acceptlist" with content "${domain1AcceptList}" of type "application/json"

    # domain2 adds domain1 to its 'follow' and 'invite-witness' accept lists.
    Given variable "domain2AcceptList" is assigned the JSON value '[{"type":"follow","add":["${domain1ID}"]},{"type":"invite-witness","add":["${domain1ID}","${domain4ID}"]}]'
    When an HTTP POST is sent to "${domain2IRI}/acceptlist" with content "${domain2AcceptList}" of type "application/json"

    When an HTTP GET is sent to "${domain1IRI}/acceptlist"
    Then the JSON path '#(type="follow").url' of the response contains "${domain2ID}"
    Then the JSON path '#(type="follow").url' of the response contains "${domain3ID}"
    Then the JSON path '#(type="invite-witness").url' of the response contains "${domain2ID}"
    Then the JSON path '#(type="invite-witness").url' of the response contains "${domain3ID}"

    # domain2 server follows domain1 server
    And variable "followActivity" is assigned the JSON value '{"@context":"https://www.w3.org/ns/activitystreams","type":"Follow","actor":"${domain2ID}","to":"${domain1IRI}","object":"${domain1ID}"}'
    When an HTTP POST is sent to "${domain2IRI}/outbox" with content "${followActivity}" of type "application/json"

    # domain1 server follows domain2 server
    And variable "followActivity" is assigned the JSON value '{"@context":"https://www.w3.org/ns/activitystreams","type":"Follow","actor":"${domain1ID}","to":"${domain2IRI}","object":"${domain2ID}"}'
    When an HTTP POST is sent to "${domain1IRI}/outbox" with content "${followActivity}" of type "application/json"

    # domain3 server follows domain1 server. Domain3 needs to be a follower of domain1 so that HTTP signature validation succeeds when
    # the /cas endpoint is invoked on domain1 (since only followers and witnesses are authorized).
    And variable "followActivity" is assigned the JSON value '{"@context":"https://www.w3.org/ns/activitystreams","type":"Follow","actor":"${domain3ID}","to":"${domain1IRI}","object":"${domain1ID}"}'
    When an HTTP POST is sent to "${domain3IRI}/outbox" with content "${followActivity}" of type "application/json"

    # domain1 invites domain2 to be a witness
    And variable "inviteWitnessActivity" is assigned the JSON value '{"@context":["https://www.w3.org/ns/activitystreams","https://w3id.org/activityanchors/v1"],"type":"Invite","actor":"${domain1ID}","to":"${domain2IRI}","object":"https://w3id.org/activityanchors#AnchorWitness","target":"${domain2ID}"}'
    When an HTTP POST is sent to "${domain1IRI}/outbox" with content "${inviteWitnessActivity}" of type "application/json"

    # domain2 invites domain1 to be a witness
    And variable "inviteWitnessActivity" is assigned the JSON value '{"@context":["https://www.w3.org/ns/activitystreams","https://w3id.org/activityanchors/v1"],"type":"Invite","actor":"${domain2ID}","to":"${domain1IRI}","object":"https://w3id.org/activityanchors#AnchorWitness","target":"${domain1ID}"}'
    When an HTTP POST is sent to "${domain2IRI}/outbox" with content "${inviteWitnessActivity}" of type "application/json"

    # domain3 invites domain1 to be a witness
    And variable "inviteWitnessActivity" is assigned the JSON value '{"@context":["https://www.w3.org/ns/activitystreams","https://w3id.org/activityanchors/v1"],"type":"Invite","actor":"${domain3ID}","to":"${domain1IRI}","object":"https://w3id.org/activityanchors#AnchorWitness","target":"${domain1ID}"}'
    When an HTTP POST is sent to "${domain3IRI}/outbox" with content "${inviteWitnessActivity}" of type "application/json"

    # domain4 invites domain3 to be a witness
    And variable "inviteWitnessActivity" is assigned the JSON value '{"@context":["https://www.w3.org/ns/activitystreams","https://w3id.org/activityanchors/v1"],"type":"Invite","actor":"${domain4ID}","to":"${domain3IRI}","object":"https://w3id.org/activityanchors#AnchorWitness","target":"${domain3ID}"}'
    When an HTTP POST is sent to "${domain4IRI}/outbox" with content "${inviteWitnessActivity}" of type "application/json"

    # domain4 invites domain2 to be a witness
    And variable "inviteWitnessActivity" is assigned the JSON value '{"@context":["https://www.w3.org/ns/activitystreams","https://w3id.org/activityanchors/v1"],"type":"Invite","actor":"${domain4ID}","to":"${domain2IRI}","object":"https://w3id.org/activityanchors#AnchorWitness","target":"${domain2ID}"}'
    When an HTTP POST is sent to "${domain4IRI}/outbox" with content "${inviteWitnessActivity}" of type "application/json"

    # set witness policy for domain1
    When an HTTP POST is sent to "https://orb.domain1.com/policy" with content "MinPercent(100,batch) AND MinPercent(50,system)" of type "text/plain"

    # set witness policy for domain4
    When an HTTP POST is sent to "https://orb.domain4.com/policy" with content "MinPercent(100,batch) AND MinPercent(50,system)" of type "text/plain"

    Then we wait 3 seconds

  @vct_log_rotation_REST_test
  Scenario: rotate VCT log
    Given the authorization bearer token for "POST" requests to path "/log" is set to "ADMIN_TOKEN"
    And the authorization bearer token for "POST" requests to path "/log-monitor" is set to "ADMIN_TOKEN"

    When an HTTP GET is sent to "https://orb.domain1.com/log-monitor"
    When an HTTP GET is sent to "https://orb.domain1.com/log-monitor?status=active"
    When an HTTP GET is sent to "https://orb.domain1.com/log-monitor?status=inactive" and the returned status code is 404

    # domain1 will start following 2022 VCT log
    When an HTTP POST is sent to "https://orb.domain1.com/log" with content "http://orb.vct:8077/maple2022" of type "text/plain"

    Then we wait 2 seconds
    When an HTTP GET is sent to "https://orb.domain1.com/log-monitor?status=active"
    And the JSON path "active.#.logUrl" of the response contains "http://orb.vct:8077/maple2022"

    And variable "activateLog" is assigned the JSON value '{"activate":["http://orb.vct:8077/maple2022"]}'
    And variable "deactivateLog" is assigned the JSON value '{"deactivate":["http://orb.vct:8077/maple2022"]}'

    # next step would be to provide endpoint for listing active and inactive logs in order to check the following two steps
    When an HTTP POST is sent to "https://orb.domain1.com/log-monitor" with content "${deactivateLog}" of type "text/plain"

    Then we wait 1 seconds
    When an HTTP GET is sent to "https://orb.domain1.com/log-monitor?status=inactive"
    And the JSON path "inactive.#.logUrl" of the response contains "http://orb.vct:8077/maple2022"

    When an HTTP POST is sent to "https://orb.domain1.com/log-monitor" with content "${activateLog}" of type "text/plain"

    Then we wait 1 seconds
    When an HTTP GET is sent to "https://orb.domain1.com/log-monitor?status=active"
    And the JSON path "active.#.logUrl" of the response contains "http://orb.vct:8077/maple2022"

  @all
  @discover_did_hashlink
  Scenario: discover did (hashlink)
    When client discover orb endpoints

      # orb-domain1 keeps accepting requests
    When client sends request to "https://orb.domain1.com/sidetree/v1/operations" to create DID document
    Then check success response contains "#interimDID"

    Then we wait 2 seconds
    When client sends request to "https://orb.domain1.com/sidetree/v1/identifiers" to resolve DID document with interim did
    Then check success response contains "canonicalId"

    When client sends request to "https://orb.domain1.com/sidetree/v1/identifiers" to resolve DID document with canonical did
    Then check success response contains "#canonicalDID"

    When client sends request to "https://orb.domain1.com/sidetree/v1/operations" to recover DID document
    Then check for request success
    Then we wait 2 seconds

    When client sends request to "https://orb.domain1.com/sidetree/v1/identifiers" to resolve DID document with canonical did
    Then check success response contains "recoveryKey"
    Then check success response contains "#canonicalDID"

      # resolve did in domain4 - it will trigger did discovery in different organisations
    When client sends request to "https://orb.domain4.com/sidetree/v1/identifiers" to resolve DID document with equivalent did
    Then check error response contains "not found"

    Then we wait 3 seconds
    When client sends request to "https://orb.domain4.com/sidetree/v1/identifiers" to resolve DID document with equivalent did
    Then check success response contains "#canonicalDID"
    Then check success response contains "recoveryKey"

    When client sends request to "https://orb.domain4.com/sidetree/v1/identifiers" to resolve DID document with canonical did
    Then check success response contains "#canonicalDID"
    Then check success response contains "recoveryKey"

  @all
  @discover_did_https
  Scenario: discover did (https)
    When client discover orb endpoints

      # orb-domain1 keeps accepting requests
    When client sends request to "https://orb.domain1.com/sidetree/v1/operations" to create DID document
    Then check success response contains "#interimDID"

    Then we wait 2 seconds
    When client sends request to "https://orb.domain1.com/sidetree/v1/identifiers" to resolve DID document with interim did
    Then check success response contains "canonicalId"

    When client sends request to "https://orb.domain1.com/sidetree/v1/identifiers" to resolve DID document with canonical did
    Then check success response contains "#canonicalDID"

    When client sends request to "https://orb.domain1.com/sidetree/v1/operations" to recover DID document
    Then check for request success
    Then we wait 2 seconds

    When client sends request to "https://orb.domain1.com/sidetree/v1/identifiers" to resolve DID document with canonical did
    Then check success response contains "recoveryKey"
    Then check success response contains "#canonicalDID"

      # resolve did in domain4 - it will trigger did discovery in different organisations
    When client sends request to "https://orb.domain4.com/sidetree/v1/identifiers" to resolve DID document with hint "https:orb.domain1.com"
    Then check error response contains "not found"

    Then we wait 3 seconds
    When client sends request to "https://orb.domain4.com/sidetree/v1/identifiers" to resolve DID document with hint "https:orb.domain1.com"
    Then check success response contains "#canonicalDID"
    Then check success response contains "recoveryKey"

    When client sends request to "https://orb.domain4.com/sidetree/v1/identifiers" to resolve DID document with canonical did
    Then check success response contains "#canonicalDID"
    Then check success response contains "recoveryKey"

  @all
  @discover_did_ipfs
  Scenario: discover did (ipfs)
    When client discover orb endpoints

      # orb-domain1 keeps accepting requests
    When client sends request to "https://orb.domain1.com/sidetree/v1/operations" to create DID document
    Then check success response contains "#interimDID"

    Then we wait 2 seconds
    When client sends request to "https://orb.domain1.com/sidetree/v1/identifiers" to resolve DID document with interim did
    Then check success response contains "canonicalId"

    When client sends request to "https://orb.domain1.com/sidetree/v1/identifiers" to resolve DID document with canonical did
    Then check success response contains "#canonicalDID"

    When client sends request to "https://orb.domain1.com/sidetree/v1/operations" to add public key with ID "firstKey" to DID document
    Then check for request success
    Then we wait 2 seconds

    When client sends request to "https://orb.domain1.com/sidetree/v1/identifiers" to resolve DID document with canonical did
    Then check success response contains "firstKey"
    Then check success response contains "#canonicalDID"

      # resolve did in domain4 - it will trigger did discovery in different organisations
    When client sends request to "https://orb.domain4.com/sidetree/v1/identifiers" to resolve DID document with hint "ipfs"
    Then check error response contains "not found"

    Then we wait 3 seconds
    When client sends request to "https://orb.domain4.com/sidetree/v1/identifiers" to resolve DID document with hint "ipfs"
    Then check success response contains "#canonicalDID"
    Then check success response contains "firstKey"

    When client sends request to "https://orb.domain4.com/sidetree/v1/identifiers" to resolve DID document with canonical did
    Then check success response contains "#canonicalDID"
    Then check success response contains "firstKey"

  @all
  @resolve_from_anchor_origin
  Scenario: discover did followed by resolve from anchor origin
    When client discover orb endpoints

      # create document in domain3
    When client sends request to "https://orb.domain3.com/sidetree/v1/operations" to create DID document
    Then check success response contains "#interimDID"

    Then we wait 3 seconds
    When client sends request to "https://orb.domain3.com/sidetree/v1/identifiers" to resolve DID document with interim did
    Then check success response contains "canonicalId"

    When client sends request to "https://orb.domain3.com/sidetree/v1/identifiers" to resolve DID document with canonical did
    Then check success response contains "#canonicalDID"

      # resolve did in domain4 - it will trigger did discovery from domain3
    When client sends request to "https://orb.domain4.com/sidetree/v1/identifiers" to resolve DID document with hint "https:orb.domain3.com"
    Then check error response contains "not found"

    Then we wait 3 seconds
    When client sends request to "https://orb.domain4.com/sidetree/v1/identifiers" to resolve DID document with canonical did
    Then check success response contains "#canonicalDID"

      # update domain3 document
    When client sends request to "https://orb.domain3.com/sidetree/v1/operations" to add public key with ID "firstKey" to DID document
    Then check for request success

    # resolution from domain4 will contain unpublished update operation from domain3
    # because domain4 has resolve from anchor origin property set to true
    When client sends request to "https://orb.domain4.com/sidetree/v1/identifiers" to resolve DID document with canonical did
    Then check success response contains "firstKey"

    # send an update operation to domain4 (anchor origin domain has unpublished operations)
    When client sends request to "https://orb.domain4.com/sidetree/v1/operations" to add public key with ID "secondKey" to DID document
    Then check error response contains "anchor origin has unpublished operations - please re-submit your request at later time"

    # wait for domain3 to publish operation
    Then we wait 5 seconds

    # re-request resolution from domain4 that doesn't have published operation since it doesn't follow any domain
    # response will contain published operation from anchor origin
    When client sends request to "https://orb.domain4.com/sidetree/v1/identifiers" to resolve DID document with canonical did
    Then check success response contains "firstKey"

    # send an update operation to domain4 (out of sync with anchor origin domain)
    When client sends request to "https://orb.domain4.com/sidetree/v1/operations" to add public key with ID "secondKey" to DID document
    Then check error response contains "anchor origin has additional published operations - please re-submit your request at later time"

  @all
  @follow_anchor_writer_domain1
  Scenario: domain2 server follows domain1 server (anchor writer)

      # send create request to domain1
    When client sends request to "https://orb.domain1.com/sidetree/v1/operations" to create DID document
    Then check success response contains "#interimDID"

    Then we wait 2 seconds
    When client sends request to "https://orb.domain1.com/sidetree/v1/identifiers" to resolve DID document with interim did
    Then check success response contains "#canonicalDID"

      # check that document is available on the first server of domain2
    Then we wait 2 seconds
    When client sends request to "https://orb.domain2.com/sidetree/v1/identifiers" to resolve DID document with canonical did
    Then check success response contains "#canonicalDID"

  @all
  @follow_anchor_writer_domain2
  Scenario: domain1 server follows domain2 server (anchor writer)

      # send create request to domain2
    When client sends request to "https://orb.domain2.com/sidetree/v1/operations" to create DID document
    Then check success response contains "#interimDID"

    Then we wait 2 seconds
    When client sends request to "https://orb.domain2.com/sidetree/v1/identifiers" to resolve DID document with interim did
    Then check success response contains "#canonicalDID"

      # check that document is available on the first server of domain1
    Then we wait 2 seconds
    When client sends request to "https://orb.domain1.com/sidetree/v1/identifiers" to resolve DID document with canonical did
    Then check success response contains "#canonicalDID"

      # check that document is available on the second server of domain1
    When client sends request to "https://orb2.domain1.com/sidetree/v1/identifiers" to resolve DID document with canonical did
    Then check success response contains "#canonicalDID"

  @all
  @concurrent_requests_scenario
  Scenario: concurrent requests plus server shutdown tests

     # write batch of DIDs to multiple servers and check them
    When client sends request to "https://orb2.domain1.com/sidetree/v1/operations,https://orb.domain2.com/sidetree/v1/operations" to create 50 DID documents using 10 concurrent requests

     # Stop orb2.domain1. The other instance in the domain should process any pending operations since
     # we're using a durable operation queue.
    Then container "orb2-domain1" is stopped
    And we wait 3 seconds

    Then client sends request to "https://orb.domain1.com/sidetree/v1/identifiers,https://orb.domain2.com/sidetree/v1/identifiers" to verify the DID documents that were created

    Then container "orb-domain1" is stopped
    Then container "orb-domain2" is stopped
    Then container "ipfs" is stopped

    Then container "orb-domain1" is started
    Then container "orb2-domain1" is started
    Then container "orb-domain2" is started
    Then container "ipfs" is started

    Then we wait 10 seconds

     # now that servers re-started we should check for DIDs that were created before shutdowns
    Then client sends request to "https://orb.domain1.com/sidetree/v1/identifiers,https://orb2.domain1.com/sidetree/v1/identifiers,https://orb.domain2.com/sidetree/v1/identifiers" to verify the DID documents that were created

     # write batch of DIDs to multiple servers again and check them
    When client sends request to "https://orb.domain1.com/sidetree/v1/operations,https://orb2.domain1.com/sidetree/v1/operations,https://orb.domain2.com/sidetree/v1/operations" to create 50 DID documents using 10 concurrent requests

    Then we wait 5 seconds
    Then client sends request to "https://orb.domain3.com/sidetree/v1/identifiers,https://orb.domain1.com/sidetree/v1/identifiers,https://orb2.domain1.com/sidetree/v1/identifiers,https://orb.domain2.com/sidetree/v1/identifiers" to verify the DID documents that were created

    When an HTTP GET is sent to "${domain1IRI}/liked?page=true"
    Then the JSON path "type" of the response equals "OrderedCollectionPage"
    And the JSON path "orderedItems" of the array response is not empty
    And the JSON path "orderedItems.0" of the response is saved to variable "anchorLink"
    And variable "anchorLinkEncoded" is assigned the value "$URLEncode(|${anchorLink}|)"

    And variable "anchorHash" is assigned the value "$hashlink(|${anchorLink}|).ResourceHash"

    When an HTTP GET is sent to "https://orb.domain1.com/cas/${anchorHash}"
    Then the JSON path "index" of the response is saved to variable "anchorIndex"

    # Get the hashlink of the anchor Linkset content from the index
    Then the JSON path 'linkset.0.replies.0.href' of the response is saved to variable "vcDataURI"
    # Decode the data URI to get the content.
    Then the data URI "${vcDataURI}" is resolved and saved to variable "vcContent"
    Then the JSON path "id" of document '${vcContent}' is saved to variable "vcID"

    # Ensure the verifiable credential can be retrieved by ID
    When an HTTP GET is sent to "${vcID}"
    Then the JSON path "id" of the response equals "${vcID}"

    When an HTTP GET is sent to "${domain2IRI}/likes/${anchorLinkEncoded}?page=true"
    Then the JSON path "type" of the response equals "OrderedCollectionPage"
     # There should be two Like's:
     # 1 - From domain1 (which received the 'Create');
     # 2 - From domain3 (which received the 'Announce')
    And the JSON path "orderedItems.#" of the response has 2 items
    And the JSON path "orderedItems.#.actor" of the response contains "${domain1ID}"
    And the JSON path "orderedItems.#.actor" of the response contains "${domain3ID}"
    And the JSON path "orderedItems.0.object.url" of the response equals "${anchorLink}"
    And the JSON path 'orderedItems.#(actor="${domain1ID}")' of the raw response is saved to variable "likeActivity"

    When an HTTP GET is sent to "${domain1IRI}/likes/${anchorLinkEncoded}?page=true"
    Then the JSON path "type" of the response equals "OrderedCollectionPage"
     # There should be one Like:
     # 1 - From domain3 (which received the 'Announce')
    And the JSON path "orderedItems.#" of the response has 1 items
    And the JSON path "orderedItems.#.actor" of the response contains "${domain3ID}"
    And the JSON path "orderedItems.0.object.url" of the response equals "${anchorLink}"

    Given variable "undoLikeActivity" is assigned the JSON value '{"@context":"https://www.w3.org/ns/activitystreams","type":"Undo","actor":"${domain1ID}","to":"${domain2IRI}","object":#{likeActivity}}'
    When an HTTP POST is sent to "${domain1IRI}/outbox" with content "${undoLikeActivity}" of type "application/json"

    Then we wait 2 seconds

    When an HTTP GET is sent to "${domain1IRI}/liked?page=true"
    Then the JSON path "orderedItems.0.object.url" of the response does not contain "${anchorLink}"

    When an HTTP GET is sent to "${domain2IRI}/likes/${anchorLinkEncoded}?page=true"
    Then the JSON path "orderedItems.#" of the response has 1 items
    And the JSON path "orderedItems.#.actor" of the response contains "${domain3ID}"

  @all
  @resolve_unpublished_create
  Scenario: domain4 has both create and update operations enabled (by default) for unpublished operation store

    When client sends request to "https://orb.domain4.com/sidetree/v1/operations" to create DID document
    Then check success response contains "#interimDID"

      # since domain4 has create operation enabled we are able to resolve did document immediately from the store
    When client sends request to "https://orb.domain4.com/sidetree/v1/identifiers" to resolve DID document with interim did
    Then check success response contains "#interimDID"
    Then check success response does NOT contain "canonicalId"

    Then client verifies resolved document

    When client sends request to "https://orb.domain4.com/sidetree/v1/identifiers" to resolve DID document with equivalent did
    Then check success response does NOT contain "canonicalId"

    Then client verifies resolved document

    # repeat same request with hint(additional check)
    When client sends request to "https://orb.domain4.com/sidetree/v1/identifiers" to resolve interim DID document with hint "https:orb.domain3.com"
    Then check success response does NOT contain "canonicalId"

    Then client verifies resolved document

    Then mis-configured client fails to verify resolved document

    Then we wait 6 seconds
    When client sends request to "https://orb.domain4.com/sidetree/v1/identifiers" to resolve DID document with interim did
    Then check success response contains "canonicalId"

    Then client verifies resolved document

  @all
  @resolve_unpublished_update_with_published_create
  Scenario: domain3 has only update operation enabled for unpublished operation store which means that create has to be anchored/published first

    When client sends request to "https://orb.domain3.com/sidetree/v1/operations" to create DID document
    Then check success response contains "#interimDID"

    # since domain4 has create document store enabled we are able to resolve did document immediately from the store
    When client sends request to "https://orb.domain3.com/sidetree/v1/identifiers" to resolve DID document with equivalent did
    Then check error response contains "not found"

    When client sends request to "https://orb.domain3.com/sidetree/v1/identifiers" to resolve DID document with equivalent did
    Then check success response contains "canonicalId"

    Then client verifies resolved document

    When client sends request to "https://orb.domain3.com/sidetree/v1/operations" to add public key with ID "firstKey" to DID document
    Then check for request success

    # update request can be immediately resolved (equivalent did)
    When client sends request to "https://orb.domain3.com/sidetree/v1/identifiers" to resolve DID document with equivalent did
    Then check success response contains "createKey"
    Then check success response contains "firstKey"

    # update request can be immediately resolved (canonical did)
    When client sends request to "https://orb.domain3.com/sidetree/v1/identifiers" to resolve DID document with canonical did
    Then check success response contains "createKey"
    Then check success response contains "firstKey"

    Then client verifies resolved document

    # make sure that all operations have been processed - no unpublished operations
    When client sends request to "https://orb.domain3.com/sidetree/v1/identifiers" to resolve DID document with canonical did
    Then check success response does NOT contain "unpublishedOperations"
    Then check success response contains "createKey"
    Then check success response contains "firstKey"

  @all
  @standalone_domain
  Scenario: Standalone domain
    # Set the witness policy to require no external proofs since this is a stand-alone domain where
    # no witnesses or followers are configured.
    When an HTTP POST is sent to "https://orb.domain5.com/policy" with content "OutOf(0,system)" of type "text/plain"
    Then an HTTP GET is sent to "https://orb.domain5.com/policy"
    And the response equals "OutOf(0,system)"

    Then client sends request to "https://orb.domain5.com/sidetree/v1/operations" to create 50 DID documents using 10 concurrent requests
    And client sends request to "https://orb.domain5.com/sidetree/v1/identifiers" to verify the DID documents that were created

  @local_cas
  @create_followed_by_immediate_update
  Scenario: domain4 has both create and update operations enabled (by default) for unpublished operation store

    When client sends request to "https://orb.domain4.com/sidetree/v1/operations" to create DID document
    Then check success response contains "#interimDID"

    #  we are able to resolve did document immediately from the store
    When client sends request to "https://orb.domain4.com/sidetree/v1/identifiers" to resolve DID document with interim did
    Then check success response contains "#interimDID"
    Then check success response does NOT contain "canonicalId"

    When client sends request to "https://orb.domain4.com/sidetree/v1/operations" to add public key with ID "firstKey" to DID document
    Then check for request success

    # update request can be immediately resolved
    When client sends request to "https://orb.domain4.com/sidetree/v1/identifiers" to resolve DID document with equivalent did
    Then check success response contains "firstKey"

    Then client verifies resolved document

    When client sends request to "https://orb.domain4.com/sidetree/v1/identifiers" to resolve DID document with equivalent did
    Then check success response contains "canonicalId"

    Then client verifies resolved document

    When client sends request to "https://orb.domain4.com/sidetree/v1/operations" to add public key with ID "secondKey" to DID document
    Then check for request success

    # resolve second update right away
    When client sends request to "https://orb.domain4.com/sidetree/v1/identifiers" to resolve DID document with canonical did
    Then check success response contains "secondKey"

    # at this point both first and second key are not processed so it will take couple of batches for operations to be processed
    Then we wait 10 seconds

    # make sure that all operations have been processed - no unpublished operations
    When client sends request to "https://orb.domain4.com/sidetree/v1/identifiers" to resolve DID document with canonical did
    Then check success response does NOT contain "unpublishedOperations"
    Then check success response contains "createKey"
    Then check success response contains "firstKey"
    Then check success response contains "secondKey"

    Then client verifies resolved document

  @local_cas
  @enable_update_document_store
  Scenario: various 'failure' scenarios for unpublished/published stores
    When client sends request to "https://orb.domain4.com/sidetree/v1/operations" to create DID document
    Then check success response contains "#interimDID"

      # since domain4 has create document store enabled we are able to resolve did document immediately from the store
    When client sends request to "https://orb.domain4.com/sidetree/v1/identifiers" to resolve DID document with interim did
    Then check success response contains "#interimDID"
    Then check success response does NOT contain "canonicalId"

      # re-try until create operation is published
    When client sends request to "https://orb.domain4.com/sidetree/v1/identifiers" to resolve DID document with interim did
    Then check success response contains "canonicalId"

      # now that create is published we can update document
    When client sends request to "https://orb.domain4.com/sidetree/v1/operations" to add public key with ID "firstKey" to DID document
    Then check for request success

    # update request can be immediately resolved
    When client sends request to "https://orb.domain4.com/sidetree/v1/identifiers" to resolve DID document with canonical did
    Then check success response contains "firstKey"

    When client sends request to "https://orb.domain4.com/sidetree/v1/operations" to add public key with ID "secondKey" to DID document
    Then check for request success

      # resolve second update right away
    When client sends request to "https://orb.domain4.com/sidetree/v1/identifiers" to resolve DID document with canonical did
    Then check success response contains "secondKey"

     # wait for first unpublished operation (firstKey) to clear
    Then we wait 6 seconds

    Then client verifies resolved document

    Then mis-configured client fails to verify resolved document

      # wait for second unpublished operation (secondKey) to clear
    Then we wait 6 seconds

      # stop all servers of anchor origin for domain4 operations requests - document operation(s) will fail after they have
      # been added because anchor event will not get enough proofs
    Then container "orb-domain1" is stopped
    And container "orb2-domain1" is stopped
    And we wait 2 seconds

      # update document
    When client sends request to "https://orb.domain4.com/sidetree/v1/operations" to add public key with ID "thirdKey" to DID document
    Then check for request success

      # update request can be immediately resolved
    When client sends request to "https://orb.domain4.com/sidetree/v1/identifiers" to resolve DID document with canonical did
    Then check success response contains "thirdKey"

      # wait for operation to expire
    Then we wait 20 seconds

    Then container "orb-domain1" is started
    And container "orb2-domain1" is started
    Then we wait 5 seconds

    # operation expired and it was never processed so "thirdKey" is gone
    When client sends request to "https://orb.domain4.com/sidetree/v1/identifiers" to resolve DID document with canonical did
    Then check success response does NOT contain "thirdKey"

      # third operation failed during batching - we need to send next operation request with last successful commitment
    Then client sends request to "https://orb.domain4.com/sidetree/v1/identifiers" to resolve DID document with canonical did and resets keys to last successful

    When client sends request to "https://orb.domain4.com/sidetree/v1/operations" to add public key with ID "fourthKey" to DID document
    Then check for request success

    Then we wait 2 seconds

    When client sends request to "https://orb.domain4.com/sidetree/v1/identifiers" to resolve DID document with canonical did
    Then check success response contains "fourthKey"

    Then client verifies resolved document

    When client sends request to "https://orb.domain4.com/sidetree/v1/operations" to deactivate DID document
    Then check for request success
    Then we wait 2 seconds

    When client sends request to "https://orb.domain4.com/sidetree/v1/identifiers" to resolve DID document with canonical did
    Then check success response contains "deactivated"

    Then client verifies resolved document

    When client sends request to "https://orb.domain4.com/sidetree/v1/operations" to recover DID document
    Then check error response contains "document has been deactivated, no further operations are allowed"

  @local_cas
  @alternate_links_scenario
  Scenario: WebFinger query returns alternate links for "Liked" anchor credentials
    When client sends request to "https://orb.domain1.com/sidetree/v1/operations" to create DID document and the ID is saved to variable "didID"
    Then we wait 5 seconds

    When an HTTP GET is sent to "https://orb.domain1.com/.well-known/webfinger?resource=${didID}"
    And the JSON path "links.#.href" of the response contains expression ".*orb\.domain1\.com.*"
    And the JSON path "links.#.href" of the response contains expression ".*orb\.domain2\.com.*"
    And the JSON path "links.#.href" of the response contains expression ".*orb\.domain3\.com.*"
    And the JSON path 'links.#(rel=="via").href' of the response is saved to variable "anchorLink"
    And variable "anchorHash" is assigned the value "$hashlink(|${anchorLink}|).ResourceHash"
    And variable "anchorLinkEncoded" is assigned the value "$URLEncode(|${anchorLink}|)"

    # domain3 is following domain1 so it should also have the DID.
    When an HTTP GET is sent to "https://orb.domain3.com/.well-known/webfinger?resource=${didID}"
    And the JSON path "links.#.href" of the response contains expression ".*orb\.domain1\.com.*"
    And the JSON path "links.#.href" of the response contains expression ".*orb\.domain3\.com.*"

    When an HTTP GET is sent to "https://orb.domain1.com/.well-known/webfinger?resource=https://orb.domain1.com/cas/${anchorHash}"
    And the JSON path 'links.#(rel=="self").href' of the response equals "https://orb.domain1.com/cas/${anchorHash}"
    And the JSON path "links.#.href" of the response contains expression ".*orb\.domain2\.com.*"
    And the JSON path "links.#.href" of the response contains expression ".*orb\.domain3\.com.*"

    When an HTTP GET is sent to "${domain1IRI}/likes/${anchorLinkEncoded}?page=true"
    Then the JSON path "type" of the response equals "OrderedCollectionPage"
    And the JSON path "orderedItems.#" of the response has 2 items
    And the JSON path "orderedItems.0" of the raw response is saved to variable "likeActivity_1"
    And the JSON path "orderedItems.0.actor" of the response is saved to variable "likeActor_1"
    And the JSON path "orderedItems.0.to" of the raw response is saved to variable "likeTo_1"
    And the JSON path "orderedItems.1" of the raw response is saved to variable "likeActivity_2"
    And the JSON path "orderedItems.1.actor" of the response is saved to variable "likeActor_2"
    And the JSON path "orderedItems.1.to" of the raw response is saved to variable "likeTo_2"

    # Undo the 'Like's from domain1 and domain2.
    Given variable "undoLikeActivity_1" is assigned the JSON value '{"@context":"https://www.w3.org/ns/activitystreams","type":"Undo","actor":"${likeActor_1}","to":#{likeTo_1},"object":#{likeActivity_1}}'
    And variable "undoLikeActivity_2" is assigned the JSON value '{"@context":"https://www.w3.org/ns/activitystreams","type":"Undo","actor":"${likeActor_2}","to":#{likeTo_2},"object":#{likeActivity_2}}'

    Then variable "likeActorDomain_1" is assigned the value "$GetDomainFromURI(|${likeActor_1}|)"
    When an HTTP GET is sent to "${likeActorDomain_1}/.well-known/host-meta.json"
    And the JSON path 'links.#(type=="application/activity+json").href' of the response is saved to variable "likeActorHRef_1"

    Then variable "likeActorDomain_2" is assigned the value "$GetDomainFromURI(|${likeActor_2}|)"
    When an HTTP GET is sent to "${likeActorDomain_2}/.well-known/host-meta.json"
    And the JSON path 'links.#(type=="application/activity+json").href' of the response is saved to variable "likeActorHRef_2"

    When an HTTP POST is sent to "${likeActorHRef_1}/outbox" with content "${undoLikeActivity_1}" of type "application/json"
    And an HTTP POST is sent to "${likeActorHRef_2}/outbox" with content "${undoLikeActivity_2}" of type "application/json"

    Then we wait 2 seconds

    # domain2 and domain 3 should no longer appear in the response of the WebFinger DID query.
    When an HTTP GET is sent to "https://orb.domain1.com/.well-known/webfinger?resource=${didID}"
    And the JSON path "links.#.href" of the response contains expression ".*orb\.domain1\.com.*"
    And the JSON path "links.#.href" of the response does not contain expression ".*orb\.domain2\.com.*"
    And the JSON path "links.#.href" of the response does not contain expression ".*orb\.domain3\.com.*"

    # domain2 and domain 3 should no longer appear in the response of the WebFinger CAS query.
    When an HTTP GET is sent to "https://orb.domain1.com/.well-known/webfinger?resource=https://orb.domain1.com/cas/${anchorHash}"
    And the JSON path 'links.#(rel=="self").href' of the response equals "https://orb.domain1.com/cas/${anchorHash}"
    And the JSON path "links.#.href" of the response does not contain expression ".*orb\.domain2\.com.*"
    And the JSON path "links.#.href" of the response does not contain expression ".*orb\.domain3\.com.*"

  @local_cas
  @reselect_witnesses_batch_process
  Scenario: reselect witnesses that failed scenario

    # first create is just used to setup caches
    When client sends request to "https://orb.domain4.com/sidetree/v1/operations" to create DID document
    Then check success response contains "#interimDID"

    Then we wait 2 seconds

    When client sends request to "https://orb.domain4.com/sidetree/v1/identifiers" to resolve DID document with interim did
    Then check success response contains "canonicalId"

    When client sends request to "https://orb.domain4.com/sidetree/v1/operations" to create DID document
    Then check success response contains "#interimDID"

     # stop system witness for domain4 operations requests - document operation(s) will fail after they have
     # been added because anchor event will not get enough proofs
    Then container "orb-domain3" is stopped

    # wait for event status monitor process to wake-up and re-select different system witness
    Then we wait 5 seconds

    When client sends request to "https://orb.domain4.com/sidetree/v1/identifiers" to resolve DID document with interim did
    Then check success response contains "canonicalId"

    When client sends request to "https://orb.domain4.com/sidetree/v1/identifiers" to resolve DID document with canonical did
    Then check success response contains "canonicalId"

    Then container "orb-domain3" is started
    Then we wait 10 seconds

  @all
  @allowed_origins
  Scenario: Update allowed origins
    When an HTTP GET is sent to "https://orb.domain1.com/allowedorigins"
    Then the JSON path "@this" of the response contains "https://orb.domain1.com"
    And the JSON path "@this" of the response contains "did:web:orb.domain2.com:services:orb"
    And the JSON path "@this" of the response contains "ipns://k51qzi5uqu5dgkmm1afrkmex5mzpu5r774jstpxjmro6mdsaullur27nfxle1q"

    Given variable "domain1AllowedOrigins" is assigned the JSON value '{"add":["https://orb.domain3.com","https://orb.domain4.com"]}'
    When an HTTP POST is sent to "https://orb.domain1.com/allowedorigins" with content "${domain1AllowedOrigins}" of type "application/json"

    When an HTTP GET is sent to "https://orb.domain1.com/allowedorigins"
    Then the JSON path "@this" of the response contains "https://orb.domain1.com"
    And the JSON path "@this" of the response contains "did:web:orb.domain2.com:services:orb"
    And the JSON path "@this" of the response contains "ipns://k51qzi5uqu5dgkmm1afrkmex5mzpu5r774jstpxjmro6mdsaullur27nfxle1q"
    And the JSON path "@this" of the response contains "https://orb.domain3.com"
    And the JSON path "@this" of the response contains "https://orb.domain4.com"

    Given variable "domain1AllowedOrigins" is assigned the JSON value '{"add":["did:web:orb.domain5.com:services:anchor"],"remove":["https://orb.domain3.com","https://orb.domain4.com"]}'
    When an HTTP POST is sent to "https://orb.domain1.com/allowedorigins" with content "${domain1AllowedOrigins}" of type "application/json"

    When an HTTP GET is sent to "https://orb.domain1.com/allowedorigins"
    Then the JSON path "@this" of the response contains "https://orb.domain1.com"
    And the JSON path "@this" of the response contains "did:web:orb.domain2.com:services:orb"
    And the JSON path "@this" of the response contains "ipns://k51qzi5uqu5dgkmm1afrkmex5mzpu5r774jstpxjmro6mdsaullur27nfxle1q"
    And the JSON path "@this" of the response does not contain "https://orb.domain3.com"
    And the JSON path "@this" of the response does not contain "https://orb.domain4.com"
    And the JSON path "@this" of the response contains "did:web:orb.domain5.com:services:anchor"

    # Add a duplicate and remove domain5
    Given variable "domain1AllowedOrigins" is assigned the JSON value '{"add":["https://orb.domain1.com"],"remove":["did:web:orb.domain5.com:services:anchor"]}'
    When an HTTP POST is sent to "https://orb.domain1.com/allowedorigins" with content "${domain1AllowedOrigins}" of type "application/json"

    When an HTTP GET is sent to "https://orb.domain1.com/allowedorigins"
    Then the JSON path "@this" of the response contains "https://orb.domain1.com"
    And the JSON path "@this" of the response contains "did:web:orb.domain2.com:services:orb"
    And the JSON path "@this" of the response contains "ipns://k51qzi5uqu5dgkmm1afrkmex5mzpu5r774jstpxjmro6mdsaullur27nfxle1q"
    And the JSON path "@this" of the response does not contain "https://orb.domain3.com"
    And the JSON path "@this" of the response does not contain "https://orb.domain4.com"
    And the JSON path "@this" of the response does not contain "did:web:orb.domain5.com:services:anchor"
