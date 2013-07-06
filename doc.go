// salt controller
//
//saltctl is a commandline tool to interface with salt-api(1)
//
//COMMANDS
//
//    help
//Print usage help.
//
//    e[xec] target fun [arg...]
//Run function 'fun' on 'target' minion(s) with the rest of the command line
//being used as arguments. saltctl will wait up to -t seconds for the function
//to finish and return data.
//
//    i[info] target
//Return infomation about 'target' minion(s)
//
//CONFIG FILE
//
//The file 'config' in directory -c can be used to store configuration values in
//json format.
//
//Supported settings are:
//
//"server": string containing URI (excluding path) to reach salt-api server.
//
//"user": string containing a username
//
//"timeout": integer specifying maximum bunber of seconds to wait for results from an async call.
//
//"eauth": string specifying eauth module to use
//
package main
